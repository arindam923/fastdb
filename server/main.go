package main

import (
	"net/http"
	"time"

	"k8s.io/klog/v2"
)

var (
	cfg *Config
	db  *Store
	hub *Hub
)

func main() {
	cfg = ParseAndValidate()

	if err := ensureConfig(cfg); err != nil {
		klog.Exitf("Failed to initialize config: %v", err)
	}

	jwtSecret = cfg.JWTSecret

	db = NewStore(cfg.DataDir)

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

	addr := cfg.Host + ":" + cfg.Port
	klog.Infof("⚡ FlashDB listening on %s", addr)
	klog.Infof("📁 Data directory: %s", cfg.DataDir)
	if err := http.ListenAndServe(addr, corsMiddleware(mux)); err != nil {
		klog.Fatal(err)
	}
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Write([]byte(`{"status":"ok"}`))
}

func handleGet(w http.ResponseWriter, r *http.Request) {
	key := r.URL.Path[len("/get/"):]
	val, version, ok := db.Get(key)
	if !ok {
		http.Error(w, `{"error":"not found"}`, 404)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"key":"` + key + `","version":` + itoa(version) + `,"value":` + string(val) + `}`))
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
