package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  4096,
	WriteBufferSize: 4096,
	// Allow ALL origins — browsers send an Origin header on WS upgrade,
	// gorilla blocks it by default if origin != host. We override for dev.
	CheckOrigin: func(r *http.Request) bool { return true },
}

var jwtSecret string

func init() {
	jwtSecret = os.Getenv("JWT_SECRET")
	if jwtSecret == "" {
		log.Fatal("JWT_SECRET environment variable is required")
	}
}

type Claims struct {
	UserID    string `json:"sub"`
	Namespace string `json:"namespace,omitempty"`
}

func validateJWT(tokenString string) (*Claims, error) {
	token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return []byte(jwtSecret), nil
	})
	if err != nil {
		return nil, err
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok || !token.Valid {
		return nil, fmt.Errorf("invalid token")
	}

	sub, ok := claims["sub"].(string)
	if !ok {
		return nil, fmt.Errorf("missing sub claim")
	}

	ns, _ := claims["namespace"].(string)

	return &Claims{
		UserID:    sub,
		Namespace: ns,
	}, nil
}

// wsHeaders adds the headers browsers need before gorilla takes over the connection
func wsHeaders(w http.ResponseWriter) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
}

// ---- Message types ----
// Client → Server
type InMessage struct {
	ID       string          `json:"id"`
	Op       string          `json:"op"` // "get" | "set" | "cas" | "delete" | "sub" | "unsub" | "ping" | "ack"
	Key      string          `json:"key"`
	Value    json.RawMessage `json:"value,omitempty"`
	Version  int64           `json:"version,omitempty"` // for CAS
	Critical bool            `json:"critical,omitempty"`
	AckID    string          `json:"ackId,omitempty"`
}

// Server → Client
type OutMessage struct {
	ID          string          `json:"id,omitempty"`
	Op          string          `json:"op"` // "ack" | "error" | "event" | "pong"
	Key         string          `json:"key,omitempty"`
	Value       json.RawMessage `json:"value,omitempty"`
	Version     int64           `json:"version,omitempty"`
	Error       string          `json:"error,omitempty"`
	Conflict    bool            `json:"conflict,omitempty"`
	ServerMsgID string          `json:"serverMsgId,omitempty"`
}

// ---- Client ----
type Client struct {
	hub          *Hub
	conn         *websocket.Conn
	send         chan []byte
	subs         map[string]bool // keys this client is subscribed to
	subsMu       sync.RWMutex
	claims       *Claims
	msgIDCounter uint64
	pendingAcks  map[string]*pendingMessage
	pendingMu    sync.Mutex
	lastAckedID  uint64
}

type pendingMessage struct {
	id     string
	msg    []byte
	sentAt time.Time
}

func (c *Client) generateMsgID() string {
	id := atomic.AddUint64(&c.msgIDCounter, 1)
	return fmt.Sprintf("srv-%d", id)
}

func (c *Client) handleAck(ackID string) {
	c.pendingMu.Lock()
	defer c.pendingMu.Unlock()

	idNum := 0
	fmt.Sscanf(ackID, "srv-%d", &idNum)
	if idNum > int(c.lastAckedID) {
		c.lastAckedID = uint64(idNum)
	}

	delete(c.pendingAcks, ackID)
}

func (c *Client) resendPending() {
	c.pendingMu.Lock()
	defer c.pendingMu.Unlock()

	now := time.Now()
	for id, pm := range c.pendingAcks {
		if now.Sub(pm.sentAt) > 5*time.Second {
			select {
			case c.send <- pm.msg:
				pm.sentAt = now
			default:
				log.Printf("client %v slow, cannot resend %s", c, id)
			}
		}
	}
}

func (c *Client) resendLoop() {
	ticker := time.NewTicker(1 * time.Second)
	defer func() {
		ticker.Stop()
		c.hub.unregister <- c
	}()
	for {
		select {
		case <-ticker.C:
			c.resendPending()
		}
	}
}

func matchPattern(key, pattern string) bool {
	if pattern == key {
		return true
	}

	parts := strings.Split(key, "/")
	patternParts := strings.Split(pattern, "/")

	for i, pp := range patternParts {
		if pp == "**" {
			return true
		}
		if pp == "*" {
			if i >= len(parts) {
				return false
			}
			continue
		}
		if i >= len(parts) || pp != parts[i] {
			return false
		}
	}

	return len(patternParts) == len(parts)
}

func (c *Client) isSubbed(key string) bool {
	c.subsMu.RLock()
	defer c.subsMu.RUnlock()

	for pattern := range c.subs {
		if matchPattern(key, pattern) {
			return true
		}
	}
	return false
}

func (c *Client) subscribe(key string) {
	c.subsMu.Lock()
	c.subs[key] = true
	c.subsMu.Unlock()
}

func (c *Client) unsubscribe(key string) {
	c.subsMu.Lock()
	delete(c.subs, key)
	c.subsMu.Unlock()
}

const (
	writeWait  = 10 * time.Second
	pongWait   = 60 * time.Second
	pingPeriod = (pongWait * 9) / 10
	maxMsgSize = 1 << 20 // 1MB max message
)

func (c *Client) readPump() {
	defer func() {
		c.hub.unregister <- c
		c.conn.Close()
	}()
	c.conn.SetReadLimit(maxMsgSize)
	c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	for {
		_, raw, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("ws read error: %v", err)
			}
			break
		}
		c.hub.process(c, raw)
	}
}

func (c *Client) writePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()
	for {
		select {
		case msg, ok := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			if err := c.conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				return
			}
		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// ---- Hub ----
type Hub struct {
	db         *Store
	clients    map[*Client]bool
	clientsMu  sync.RWMutex
	register   chan *Client
	unregister chan *Client
	broadcast  chan broadcastEvent
}

type broadcastEvent struct {
	key     string
	payload []byte
	exclude *Client // don't echo back to sender
}

func NewHub(db *Store) *Hub {
	return &Hub{
		db:         db,
		clients:    make(map[*Client]bool),
		register:   make(chan *Client, 64),
		unregister: make(chan *Client, 64),
		broadcast:  make(chan broadcastEvent, 1024),
	}
}

func (h *Hub) Run() {
	for {
		select {
		case c := <-h.register:
			h.clientsMu.Lock()
			h.clients[c] = true
			h.clientsMu.Unlock()
			log.Printf("client connected, total=%d", len(h.clients))

		case c := <-h.unregister:
			h.clientsMu.Lock()
			if _, ok := h.clients[c]; ok {
				delete(h.clients, c)
				close(c.send)
			}
			h.clientsMu.Unlock()
			log.Printf("client disconnected, total=%d", len(h.clients))

		case ev := <-h.broadcast:
			h.clientsMu.RLock()
			for c := range h.clients {
				if c == ev.exclude {
					continue
				}
				if !c.isSubbed(ev.key) {
					continue
				}
				select {
				case c.send <- ev.payload:
				case <-time.After(100 * time.Millisecond):
					log.Printf("client %v slow, dropping event for key %s", c, ev.key)
				}
			}
			h.clientsMu.RUnlock()
		}
	}
}

func (h *Hub) HandleWS(w http.ResponseWriter, r *http.Request) {
	wsHeaders(w)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}

	token := r.URL.Query().Get("token")
	if token == "" {
		token = strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	}

	if token == "" {
		http.Error(w, `{"error": "unauthorized", "message": "token required"}`, http.StatusUnauthorized)
		return
	}

	claims, err := validateJWT(token)
	if err != nil {
		http.Error(w, `{"error": "unauthorized", "message": "invalid or expired token"}`, http.StatusUnauthorized)
		return
	}

	log.Printf("WS upgrade request from origin: %s, user: %s", r.Header.Get("Origin"), claims.UserID)
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("upgrade error: %v", err)
		return
	}
	c := &Client{
		hub:         h,
		conn:        conn,
		send:        make(chan []byte, 256),
		subs:        make(map[string]bool),
		claims:      claims,
		pendingAcks: make(map[string]*pendingMessage),
	}
	h.register <- c
	go c.writePump()
	go c.resendLoop()
	c.readPump()
}

func (h *Hub) sendTo(c *Client, msg OutMessage, critical bool) string {
	raw, err := json.Marshal(msg)
	if err != nil {
		return ""
	}

	var serverMsgID string
	if critical {
		serverMsgID = c.generateMsgID()
		msg.ServerMsgID = serverMsgID
		raw, _ = json.Marshal(msg)

		c.pendingMu.Lock()
		c.pendingAcks[serverMsgID] = &pendingMessage{
			id:     serverMsgID,
			msg:    raw,
			sentAt: time.Now(),
		}
		c.pendingMu.Unlock()
	}

	select {
	case c.send <- raw:
	default:
		h.sendTo(c, OutMessage{Op: "error", Error: "client buffer full, slow down"}, false)
	}

	return serverMsgID
}

func (h *Hub) broadcastChange(key string, value json.RawMessage, version int64, exclude *Client) {
	msg := OutMessage{
		Op:      "event",
		Key:     key,
		Value:   value,
		Version: version,
	}
	raw, _ := json.Marshal(msg)
	h.broadcast <- broadcastEvent{key: key, payload: raw, exclude: exclude}
}

// process handles a single incoming message from a client — this is where all ops happen
func (h *Hub) process(c *Client, raw []byte) {
	var msg InMessage
	if err := json.Unmarshal(raw, &msg); err != nil {
		h.sendTo(c, OutMessage{Op: "error", Error: "invalid json"}, false)
		return
	}

	// Handle ACK from client
	if msg.Op == "ack" {
		if msg.AckID != "" {
			c.handleAck(msg.AckID)
		}
		return
	}

	switch msg.Op {
	case "ping":
		h.sendTo(c, OutMessage{ID: msg.ID, Op: "pong"}, false)

	case "get":
		if msg.Key == "" {
			h.sendTo(c, OutMessage{ID: msg.ID, Op: "error", Error: "key required"}, false)
			return
		}
		val, ver, ok := h.db.Get(msg.Key)
		if !ok {
			h.sendTo(c, OutMessage{ID: msg.ID, Op: "ack", Key: msg.Key, Version: 0}, false)
			return
		}
		h.sendTo(c, OutMessage{ID: msg.ID, Op: "ack", Key: msg.Key, Value: val, Version: ver}, false)

	case "set":
		if msg.Key == "" {
			h.sendTo(c, OutMessage{ID: msg.ID, Op: "error", Error: "key required"}, false)
			return
		}
		if msg.Value == nil {
			h.sendTo(c, OutMessage{ID: msg.ID, Op: "error", Error: "value required"}, false)
			return
		}
		result := h.db.Set(msg.Key, msg.Value)
		h.sendTo(c, OutMessage{ID: msg.ID, Op: "ack", Key: result.Key, Value: result.Value, Version: result.Version}, msg.Critical)
		h.broadcastChange(result.Key, result.Value, result.Version, c)

	case "cas":
		// Compare-and-swap: safe concurrent update
		// Client sends: { op:"cas", key:"x", value:{...}, version: <expected version> }
		// If another client already updated the key (version mismatch), we return conflict=true
		// and the CURRENT value+version so the client can decide what to do (merge/retry/fail)
		if msg.Key == "" {
			h.sendTo(c, OutMessage{ID: msg.ID, Op: "error", Error: "key required"}, false)
			return
		}
		result, ok := h.db.CAS(msg.Key, msg.Value, msg.Version)
		if !ok {
			h.sendTo(c, OutMessage{
				ID:       msg.ID,
				Op:       "ack",
				Key:      result.Key,
				Value:    result.Value,
				Version:  result.Version,
				Conflict: true,
			}, msg.Critical)
			return
		}
		h.sendTo(c, OutMessage{ID: msg.ID, Op: "ack", Key: result.Key, Value: result.Value, Version: result.Version}, msg.Critical)
		h.broadcastChange(result.Key, result.Value, result.Version, c)

	case "delete":
		if msg.Key == "" {
			h.sendTo(c, OutMessage{ID: msg.ID, Op: "error", Error: "key required"}, false)
			return
		}
		ver, ok := h.db.Delete(msg.Key)
		if !ok {
			h.sendTo(c, OutMessage{ID: msg.ID, Op: "error", Error: "key not found"}, false)
			return
		}
		h.sendTo(c, OutMessage{ID: msg.ID, Op: "ack", Key: msg.Key, Version: ver}, msg.Critical)
		// Broadcast deletion as null value
		h.broadcastChange(msg.Key, json.RawMessage("null"), ver, c)

	case "sub":
		if msg.Key == "" {
			h.sendTo(c, OutMessage{ID: msg.ID, Op: "error", Error: "key required"}, false)
			return
		}
		c.subscribe(msg.Key)
		// Immediately send current value on subscribe
		val, ver, ok := h.db.Get(msg.Key)
		if ok {
			h.sendTo(c, OutMessage{ID: msg.ID, Op: "ack", Key: msg.Key, Value: val, Version: ver}, false)
		} else {
			h.sendTo(c, OutMessage{ID: msg.ID, Op: "ack", Key: msg.Key, Version: 0}, false)
		}

	case "unsub":
		c.unsubscribe(msg.Key)
		h.sendTo(c, OutMessage{ID: msg.ID, Op: "ack", Key: msg.Key}, false)

	default:
		h.sendTo(c, OutMessage{ID: msg.ID, Op: "error", Error: "unknown op: " + msg.Op}, false)
	}
}
