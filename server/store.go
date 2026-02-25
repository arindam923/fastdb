package main

import (
	"encoding/binary"
	"encoding/json"
	"os"
	"sync"
	"sync/atomic"
	"time"
)

var (
	persistFile = "data.json"
	walFile     = "wal.log"
)

const walBufferSize = 64 * 1024

type WalEntryType byte

const (
	WalOpSet    WalEntryType = 0x01
	WalOpCAS    WalEntryType = 0x02
	WalOpDelete WalEntryType = 0x03
)

type WalEntry struct {
	Type      WalEntryType
	Key       string
	Value     json.RawMessage
	Version   int64
	Timestamp time.Time
}

type Wal struct {
	file   *os.File
	mu     sync.Mutex
	buf    []byte
	offset int
	closed bool
}

type Entry struct {
	mu      sync.RWMutex
	data    json.RawMessage
	version int64
	updated time.Time
}

const numShards = 256

type shard struct {
	mu      sync.RWMutex
	entries map[string]*Entry
}

type Store struct {
	shards [numShards]shard
	wal    *Wal
}

func NewWal() *Wal {
	f, err := os.OpenFile(walFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil
	}
	return &Wal{
		file: f,
		buf:  make([]byte, 0, walBufferSize),
	}
}

func (w *Wal) writeEntry(e *WalEntry) error {
	if w == nil || w.closed {
		return nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()

	data, err := json.Marshal(e)
	if err != nil {
		return err
	}

	length := uint32(len(data))
	if err := binary.Write(w.file, binary.BigEndian, length); err != nil {
		return err
	}
	if _, err := w.file.Write(data); err != nil {
		return err
	}
	w.file.Sync()
	return nil
}

func (w *Wal) Close() {
	if w == nil || w.closed {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.file.Close()
	w.closed = true
}

func (w *Wal) ReadAll() ([]WalEntry, error) {
	if w == nil {
		return nil, nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()

	f, err := os.Open(walFile)
	if err != nil {
		return nil, nil
	}
	defer f.Close()

	var entries []WalEntry
	for {
		var length uint32
		if err := binary.Read(f, binary.BigEndian, &length); err != nil {
			break
		}
		data := make([]byte, length)
		if _, err := f.Read(data); err != nil {
			break
		}
		var e WalEntry
		if err := json.Unmarshal(data, &e); err != nil {
			continue
		}
		entries = append(entries, e)
	}
	return entries, nil
}

func (w *Wal) Truncate() error {
	if w == nil || w.closed {
		return nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.file.Close()
	os.Rename(walFile, walFile+".old")
	f, err := os.OpenFile(walFile, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}
	w.file = f
	return nil
}

func NewStore(dataDir string) *Store {
	persistFile = dataDir + "/data.json"
	walFile = dataDir + "/wal.log"

	s := &Store{}

	for i := range s.shards {
		s.shards[i].entries = make(map[string]*Entry)
	}

	s.wal = NewWal()
	s.loadFromDisk()
	s.recoverFromWal()
	return s
}

func (s *Store) shard(key string) *shard {
	// FNV-1a hash for fast, uniform distribution
	h := uint32(2166136261)
	for i := 0; i < len(key); i++ {
		h ^= uint32(key[i])
		h *= 16777619
	}
	return &s.shards[h%numShards]
}

// Get returns the raw JSON value + version for a key.
func (s *Store) Get(key string) (json.RawMessage, int64, bool) {
	sh := s.shard(key)
	sh.mu.RLock()
	e, ok := sh.entries[key]
	sh.mu.RUnlock()
	if !ok {
		return nil, 0, false
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.data, e.version, true
}

type SetResult struct {
	Key     string          `json:"key"`
	Value   json.RawMessage `json:"value"`
	Version int64           `json:"version"`
}

func (s *Store) Set(key string, value json.RawMessage) SetResult {
	sh := s.shard(key)
	sh.mu.Lock()
	e, ok := sh.entries[key]
	if !ok {
		e = &Entry{}
		sh.entries[key] = e
	}
	sh.mu.Unlock()

	e.mu.Lock()
	ver := atomic.AddInt64(&e.version, 1)
	e.data = value
	e.updated = time.Now()
	e.mu.Unlock()

	s.wal.writeEntry(&WalEntry{
		Type:      WalOpSet,
		Key:       key,
		Value:     value,
		Version:   ver,
		Timestamp: time.Now(),
	})

	return SetResult{Key: key, Value: value, Version: ver}
}

func (s *Store) CAS(key string, value json.RawMessage, expectedVersion int64) (SetResult, bool) {
	sh := s.shard(key)
	sh.mu.Lock()
	e, ok := sh.entries[key]
	if !ok {
		if expectedVersion != 0 {
			sh.mu.Unlock()
			return SetResult{}, false
		}
		e = &Entry{}
		sh.entries[key] = e
	}
	sh.mu.Unlock()

	e.mu.Lock()
	if e.version != expectedVersion {
		e.mu.Unlock()
		return SetResult{Key: key, Value: e.data, Version: e.version}, false
	}
	e.version++
	ver := e.version
	e.data = value
	e.updated = time.Now()
	e.mu.Unlock()

	s.wal.writeEntry(&WalEntry{
		Type:      WalOpCAS,
		Key:       key,
		Value:     value,
		Version:   ver,
		Timestamp: time.Now(),
	})

	return SetResult{Key: key, Value: value, Version: ver}, true
}

func (s *Store) Delete(key string) (int64, bool) {
	sh := s.shard(key)
	sh.mu.Lock()
	e, ok := sh.entries[key]
	if ok {
		delete(sh.entries, key)
	}
	sh.mu.Unlock()
	if !ok {
		return 0, false
	}
	e.mu.RLock()
	ver := e.version
	e.mu.RUnlock()

	s.wal.writeEntry(&WalEntry{
		Type:      WalOpDelete,
		Key:       key,
		Version:   ver,
		Timestamp: time.Now(),
	})

	return ver, true
}

func (s *Store) Keys() []string {
	var keys []string
	for i := range s.shards {
		s.shards[i].mu.RLock()
		for k := range s.shards[i].entries {
			keys = append(keys, k)
		}
		s.shards[i].mu.RUnlock()
	}
	return keys
}

type persistedEntry struct {
	Data    json.RawMessage `json:"data"`
	Version int64           `json:"version"`
	Updated time.Time       `json:"updated"`
}

type persistedData map[string]persistedEntry

func (s *Store) loadFromDisk() {
	data, err := os.ReadFile(persistFile)
	if err != nil {
		return
	}

	var stored persistedData
	if err := json.Unmarshal(data, &stored); err != nil {
		return
	}

	for key, entry := range stored {
		sh := s.shard(key)
		sh.mu.Lock()
		sh.entries[key] = &Entry{
			data:    entry.Data,
			version: entry.Version,
			updated: entry.Updated,
		}
		sh.mu.Unlock()
	}
}

func (s *Store) recoverFromWal() {
	entries, err := s.wal.ReadAll()
	if err != nil || entries == nil {
		return
	}

	for _, e := range entries {
		switch e.Type {
		case WalOpSet:
			sh := s.shard(e.Key)
			sh.mu.Lock()
			entry, ok := sh.entries[e.Key]
			if !ok {
				entry = &Entry{}
				sh.entries[e.Key] = entry
			}
			sh.mu.Unlock()
			entry.mu.Lock()
			entry.data = e.Value
			if e.Version > entry.version {
				entry.version = e.Version
			}
			entry.updated = e.Timestamp
			entry.mu.Unlock()

		case WalOpCAS:
			sh := s.shard(e.Key)
			sh.mu.Lock()
			entry, ok := sh.entries[e.Key]
			if !ok {
				entry = &Entry{}
				sh.entries[e.Key] = entry
			}
			sh.mu.Unlock()
			entry.mu.Lock()
			if e.Version > entry.version {
				entry.version = e.Version
				entry.data = e.Value
				entry.updated = e.Timestamp
			}
			entry.mu.Unlock()

		case WalOpDelete:
			sh := s.shard(e.Key)
			sh.mu.Lock()
			delete(sh.entries, e.Key)
			sh.mu.Unlock()
		}
	}
}

func (s *Store) SaveToDisk() {
	data := make(persistedData)
	for i := range s.shards {
		s.shards[i].mu.RLock()
		for k, e := range s.shards[i].entries {
			data[k] = persistedEntry{
				Data:    e.data,
				Version: e.version,
				Updated: e.updated,
			}
		}
		s.shards[i].mu.RUnlock()
	}

	jsonData, err := json.Marshal(data)
	if err != nil {
		return
	}

	tmpFile := persistFile + ".tmp"
	if err := os.WriteFile(tmpFile, jsonData, 0644); err != nil {
		return
	}
	os.Rename(tmpFile, persistFile)
}

func (s *Store) StartAutoPersist(interval time.Duration) {
	ticker := time.NewTicker(interval)
	go func() {
		for range ticker.C {
			s.SaveToDisk()
			s.wal.Truncate()
		}
	}()
}
