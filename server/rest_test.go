package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRESTAPI(t *testing.T) {
	// Initialize metrics for testing
	initMetrics()

	// Initialize test server with temporary directory
	config := &Config{
		Port:            "8080",
		Host:            "localhost",
		DataDir:         "test-data",
		JWTSecret:       "test-secret",
		PersistInterval: "5s",
		LogLevel:        "debug",
		ShardCount:      256,
	}

	if err := ensureConfig(config); err != nil {
		t.Fatalf("Failed to initialize config: %v", err)
	}

	db = NewStore(config.DataDir, config.ShardCount)
	db.StartAutoPersist(10 * time.Second)

	hub = NewHub(db)
	go hub.Run()

	// Create handler
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", hub.HandleWS)
	mux.HandleFunc("/health", handleHealth)
	mux.HandleFunc("/get/", handleGet)
	mux.HandleFunc("/set", handleSet)
	mux.HandleFunc("/delete/", handleDelete)
	mux.HandleFunc("/cas", handleCAS)
	mux.HandleFunc("/keys", handleKeys)
	mux.HandleFunc("/snapshot", handleSnapshot)

	handler := corsMiddleware(mux)

	t.Run("Health Check", func(t *testing.T) {
		req, err := http.NewRequest("GET", "/health", nil)
		if err != nil {
			t.Fatal(err)
		}

		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)

		if status := rr.Code; status != http.StatusOK {
			t.Errorf("Health check returned wrong status code: got %v want %v", status, http.StatusOK)
		}

		var health struct {
			Status string `json:"status"`
		}
		if err := json.Unmarshal(rr.Body.Bytes(), &health); err != nil {
			t.Errorf("Health check response invalid JSON: %v", err)
		}

		if health.Status != "ok" {
			t.Errorf("Health check status incorrect: got %q want %q", health.Status, "ok")
		}
	})

	t.Run("Set and Get Operation", func(t *testing.T) {
		testKey := "test-rest-key"
		testValue := `"test-rest-value"`

		reqBody := bytes.NewBuffer([]byte(`{"key":"` + testKey + `","value":` + testValue + `}`))
		req, err := http.NewRequest("POST", "/set", reqBody)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")

		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)

		if status := rr.Code; status != http.StatusCreated {
			t.Errorf("Set operation returned wrong status code: got %v want %v", status, http.StatusCreated)
		}

		var setResult SetResult
		if err := json.Unmarshal(rr.Body.Bytes(), &setResult); err != nil {
			t.Errorf("Set response invalid JSON: %v", err)
		}

		if setResult.Key != testKey {
			t.Errorf("Set response key incorrect: got %q want %q", setResult.Key, testKey)
		}

		req, err = http.NewRequest("GET", "/get/"+testKey, nil)
		if err != nil {
			t.Fatal(err)
		}

		rr = httptest.NewRecorder()
		handler.ServeHTTP(rr, req)

		if status := rr.Code; status != http.StatusOK {
			t.Errorf("Get operation returned wrong status code: got %v want %v", status, http.StatusOK)
		}

		var getResult struct {
			Key     string          `json:"key"`
			Value   json.RawMessage `json:"value"`
			Version int64           `json:"version"`
		}
		if err := json.Unmarshal(rr.Body.Bytes(), &getResult); err != nil {
			t.Errorf("Get response invalid JSON: %v", err)
		}

		if getResult.Key != testKey {
			t.Errorf("Get response key incorrect: got %q want %q", getResult.Key, testKey)
		}

		if string(getResult.Value) != testValue {
			t.Errorf("Get response value incorrect: got %q want %q", getResult.Value, testValue)
		}

		if getResult.Version <= 0 {
			t.Errorf("Get response version should be > 0: got %v", getResult.Version)
		}
	})

	t.Run("Keys Operation", func(t *testing.T) {
		req, err := http.NewRequest("GET", "/keys", nil)
		if err != nil {
			t.Fatal(err)
		}

		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)

		if status := rr.Code; status != http.StatusOK {
			t.Errorf("Keys operation returned wrong status code: got %v want %v", status, http.StatusOK)
		}

		var keysResult struct {
			Keys  []string `json:"keys"`
			Count int      `json:"count"`
		}
		if err := json.Unmarshal(rr.Body.Bytes(), &keysResult); err != nil {
			t.Errorf("Keys response invalid JSON: %v", err)
		}

		if keysResult.Count == 0 {
			t.Errorf("Keys operation returned 0 keys, expected at least 1")
		}

		found := false
		for _, key := range keysResult.Keys {
			if key == "test-rest-key" {
				found = true
				break
			}
		}

		if !found {
			t.Errorf("Test key not found in keys list")
		}
	})

	t.Run("Delete Operation", func(t *testing.T) {
		testKey := "test-rest-key"

		req, err := http.NewRequest("DELETE", "/delete/"+testKey, nil)
		if err != nil {
			t.Fatal(err)
		}

		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)

		if status := rr.Code; status != http.StatusOK {
			t.Errorf("Delete operation returned wrong status code: got %v want %v", status, http.StatusOK)
		}

		var deleteResult struct {
			Key     string `json:"key"`
			Version int64  `json:"version"`
		}
		if err := json.Unmarshal(rr.Body.Bytes(), &deleteResult); err != nil {
			t.Errorf("Delete response invalid JSON: %v", err)
		}

		if deleteResult.Key != testKey {
			t.Errorf("Delete response key incorrect: got %q want %q", deleteResult.Key, testKey)
		}

		req, err = http.NewRequest("GET", "/get/"+testKey, nil)
		if err != nil {
			t.Fatal(err)
		}

		rr = httptest.NewRecorder()
		handler.ServeHTTP(rr, req)

		if status := rr.Code; status != http.StatusNotFound {
			t.Errorf("Get after delete returned wrong status code: got %v want %v", status, http.StatusNotFound)
		}
	})

	t.Run("Snapshot Operation", func(t *testing.T) {
		req, err := http.NewRequest("POST", "/snapshot", nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")

		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)

		if status := rr.Code; status != http.StatusOK {
			t.Errorf("Snapshot operation returned wrong status code: got %v want %v", status, http.StatusOK)
		}

		var snapshotResult struct {
			Status string `json:"status"`
		}
		if err := json.Unmarshal(rr.Body.Bytes(), &snapshotResult); err != nil {
			t.Errorf("Snapshot response invalid JSON: %v", err)
		}

		if snapshotResult.Status != "ok" {
			t.Errorf("Snapshot status incorrect: got %q want %q", snapshotResult.Status, "ok")
		}
	})
}
