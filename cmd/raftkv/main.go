package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"

	"raftkv/internal/api"
	"raftkv/internal/persistence"
	"raftkv/internal/raft"
	"raftkv/internal/store"
)

func main() {
	id := flag.String("id", "node1", "Node ID")
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

	// Initialize StateStore
	stateStore, err := persistence.NewStateStore(*dataDir)
	if err != nil {
		log.Fatalf("Failed to initialize StateStore: %v", err)
	}

	// Recover from WAL
	log.Printf("Recovering from WAL...")
	records, err := wal.ReadAll()
	if err != nil {
		log.Fatalf("Failed to read WAL entries: %v", err)
	}

	raftLog := raft.NewRaftLog()
	var entries []raft.LogEntry
	for _, record := range records {
		var entry raft.LogEntry
		if err := json.Unmarshal(record, &entry); err != nil {
			log.Fatalf("Failed to unmarshal log entry: %v", err)
		}
		entries = append(entries, entry)
	}
	raftLog.Append(entries)
	log.Printf("Recovered %d entries into RaftLog. Last index: %d", len(entries), raftLog.LastIndex())

	// Single node cluster config for now
	config := raft.ClusterConfig{
		SelfID: raft.NodeID(*id),
		Nodes: []raft.NodeConfig{
			{ID: raft.NodeID(*id), Host: "localhost", Port: *port},
		},
	}

	// Initialize the orchestrator
	raftNode, err := raft.NewRaftNode(config, raftLog, stateStore, wal, sm)
	if err != nil {
		log.Fatalf("Failed to initialize RaftNode: %v", err)
	}

	raftNode.Start() // starts the background election timer

	// Initialize the HTTP API
	apiServer := api.NewServer(raftNode, sm)

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
