package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	_ "github.com/prometheus/client_golang/prometheus/promhttp"
	"k8s.io/klog/v2"
)

var (
	cfg *Config
	db  *Store
	hub *Hub

	// Errors for metrics tracking
	errNotFound         = errors.New("not found")
	errMethodNotAllowed = errors.New("method not allowed")
	errVersionMismatch  = errors.New("version mismatch")
	errInvalidRequest   = errors.New("invalid request")
)

func main() {
	cfg = ParseAndValidate()

	if err := ensureConfig(cfg); err != nil {
		klog.Exitf("Failed to initialize config: %v", err)
	}

	jwtSecret = cfg.JWTSecret

	// Initialize metrics
	initMetrics()

	db = NewStore(cfg.DataDir, cfg.ShardCount)

	persistInterval := 5 * time.Second
	if cfg.PersistInterval != "" {
		if d, err := time.ParseDuration(cfg.PersistInterval); err == nil {
			persistInterval = d
		}
	}
	db.StartAutoPersist(persistInterval)

	hub = NewHub(db)
	go hub.Run()

	mux := http.NewServeMux()
	mux.HandleFunc("/ws", hub.HandleWS)
	mux.HandleFunc("/health", handleHealth)
	mux.HandleFunc("/get/", handleGet)
	mux.HandleFunc("/set", handleSet)
	mux.HandleFunc("/delete/", handleDelete)
	mux.HandleFunc("/cas", handleCAS)
	mux.HandleFunc("/keys", handleKeys)
	mux.HandleFunc("/snapshot", handleSnapshot)
	mux.Handle("/metrics", MetricsHandler())

	addr := cfg.Host + ":" + cfg.Port
	klog.Infof("⚡ FlashDB listening on %s", addr)
	klog.Infof("📁 Data directory: %s", cfg.DataDir)
	klog.Infof("🔒 TLS enabled: %v", cfg.TLS)
	klog.Infof("🔄 Shard count: %d", cfg.ShardCount)

	if cfg.TLS {
		if err := http.ListenAndServeTLS(addr, cfg.CertFile, cfg.KeyFile, corsMiddleware(mux)); err != nil {
			klog.Fatal(err)
		}
	} else {
		if err := http.ListenAndServe(addr, corsMiddleware(mux)); err != nil {
			klog.Fatal(err)
		}
	}
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Write([]byte(`{"status":"ok"}`))
}

func handleGet(w http.ResponseWriter, r *http.Request) {
	defer RecordOperation("get", nil)()

	key := r.URL.Path[len("/get/"):]
	klog.Infof("Get key: %s", key)
	val, version, ok := db.Get(key)
	if !ok {
		klog.Infof("Key not found: %s", key)
		RecordOperation("get", errNotFound)
		http.Error(w, `{"error":"not found"}`, 404)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"key":"` + key + `","version":` + itoa(version) + `,"value":` + string(val) + `}`))
}

func handleSet(w http.ResponseWriter, r *http.Request) {
	defer RecordOperation("set", nil)()

	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, 405)
		RecordOperation("set", errMethodNotAllowed)
		return
	}

	var req struct {
		Key   string          `json:"key"`
		Value json.RawMessage `json:"value"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		klog.Errorf("Error decoding set request: %v", err)
		http.Error(w, `{"error":"invalid request"}`, 400)
		RecordOperation("set", err)
		return
	}

	result := db.Set(req.Key, req.Value)
	klog.Infof("Set key: %s, version: %d", req.Key, result.Version)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(result)
}

func handleDelete(w http.ResponseWriter, r *http.Request) {
	defer RecordOperation("delete", nil)()

	if r.Method != http.MethodDelete {
		http.Error(w, `{"error":"method not allowed"}`, 405)
		RecordOperation("delete", errMethodNotAllowed)
		return
	}

	key := r.URL.Path[len("/delete/"):]
	version, ok := db.Delete(key)
	if !ok {
		http.Error(w, `{"error":"not found"}`, 404)
		RecordOperation("delete", errNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"key":"` + key + `","version":` + itoa(version) + `}`))
}

func handleCAS(w http.ResponseWriter, r *http.Request) {
	defer RecordOperation("cas", nil)()

	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, 405)
		RecordOperation("cas", errMethodNotAllowed)
		return
	}

	var req struct {
		Key             string          `json:"key"`
		Value           json.RawMessage `json:"value"`
		ExpectedVersion int64           `json:"expectedVersion"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request"}`, 400)
		RecordOperation("cas", err)
		return
	}

	result, ok := db.CAS(req.Key, req.Value, req.ExpectedVersion)
	if !ok {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"key":      req.Key,
			"value":    string(result.Value),
			"version":  result.Version,
			"error":    "version mismatch",
			"conflict": true,
		})
		RecordOperation("cas", errVersionMismatch)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(result)
}

func handleKeys(w http.ResponseWriter, r *http.Request) {
	defer RecordOperation("keys", nil)()

	if r.Method != http.MethodGet {
		http.Error(w, `{"error":"method not allowed"}`, 405)
		RecordOperation("keys", errMethodNotAllowed)
		return
	}

	keys := db.Keys()
	klog.Infof("Keys count: %d", len(keys))
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"keys":  keys,
		"count": len(keys),
	})
}

func handleSnapshot(w http.ResponseWriter, r *http.Request) {
	defer RecordOperation("snapshot", nil)()

	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, 405)
		RecordOperation("snapshot", errMethodNotAllowed)
		return
	}

	if err := db.SaveSnapshot(); err != nil {
		http.Error(w, `{"error":"failed to save snapshot"}`, 500)
		RecordOperation("snapshot", err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"status":"ok"}`))
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == http.MethodOptions {
			return
		}
		next.ServeHTTP(w, r)
	})
}

func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	buf := make([]byte, 0, 20)
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		buf = append([]byte{byte('0' + n%10)}, buf...)
		n /= 10
	}
	if neg {
		buf = append([]byte{'-'}, buf...)
	}
	return string(buf)
}
