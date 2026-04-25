package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"sync/atomic"

	"raftkv/internal/api"
	"raftkv/internal/persistence"
	"raftkv/internal/store"
)

func main() {
	port := flag.Int("port", 3001, "HTTP server port")
	dataDir := flag.String("data", "data", "Data directory")
	flag.Parse()

	// Initialize the Phase 1 state machine
	sm := store.NewStateMachine()

	// Initialize WAL
	wal, err := persistence.NewWAL(*dataDir)
	if err != nil {
		log.Fatalf("Failed to initialize WAL: %v", err)
	}
	defer wal.Close()

	// Recover from WAL
	log.Printf("Recovering from WAL...")
	entries, err := wal.ReadEntries()
	if err != nil {
		log.Fatalf("Failed to read WAL entries: %v", err)
	}

	var lastIndex atomic.Uint64

	for _, entry := range entries {
		var cmd store.Command
		if err := json.Unmarshal(entry.Command, &cmd); err != nil {
			log.Fatalf("Failed to unmarshal command at index %d: %v", entry.Index, err)
		}
		sm.Apply(cmd)
		lastIndex.Store(uint64(entry.Index))
	}
	log.Printf("Recovered %d entries. Last index: %d", len(entries), lastIndex.Load())

	// Initialize the HTTP API
	apiServer := api.NewServer(sm, wal, &lastIndex)

	// Register routes
	mux := http.NewServeMux()
	apiServer.RegisterRoutes(mux)

	// Phase 0 endpoints
	mux.HandleFunc("GET /ping", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("pong"))
	})
	mux.HandleFunc("GET /status", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	})

	addr := fmt.Sprintf(":%d", *port)
	log.Printf("Starting RaftKV server on %s", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}
