package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"

	"raftkv/internal/api"
	"raftkv/internal/persistence"
	"raftkv/internal/raft"
	"raftkv/internal/rpc"
	"raftkv/internal/store"
)

func main() {
	id := flag.String("id", "node1", "Node ID")
	port := flag.Int("port", 3001, "HTTP server port")
	dataDir := flag.String("data", "data", "Data directory")
	peersFlag := flag.String("peers", "", "Comma-separated list of peers (e.g. node2=localhost:3002,node3=localhost:3003)")
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

	// Replay committed entries into State Machine
	var lastApplied raft.Index = 0
	// For now, we assume everything in the WAL was committed on restart.
	// In a full Raft implementation, this would be drive by the persistent commitIndex or Snapshot.
	for _, entry := range entries {
		var cmd store.Command
		if err := json.Unmarshal(entry.Command, &cmd); err != nil {
			log.Printf("Warning: Failed to unmarshal command at index %d: %v", entry.Index, err)
			continue
		}
		sm.Apply(cmd)
		lastApplied = entry.Index
	}
	if lastApplied > 0 {
		log.Printf("Replayed %d entries into State Machine. Current index: %d", len(entries), lastApplied)
	}

	// Build ClusterConfig
	var nodes []raft.NodeConfig
	nodes = append(nodes, raft.NodeConfig{
		ID:   raft.NodeID(*id),
		Host: "localhost",
		Port: *port,
	})

	if *peersFlag != "" {
		peerPairs := strings.Split(*peersFlag, ",")
		for _, pair := range peerPairs {
			parts := strings.Split(pair, "=")
			if len(parts) != 2 {
				log.Fatalf("Invalid peer format: %s", pair)
			}
			peerID := parts[0]
			address := parts[1] // e.g. localhost:3002

			hostPort := strings.Split(address, ":")
			if len(hostPort) != 2 {
				log.Fatalf("Invalid address format: %s", address)
			}
			p, err := strconv.Atoi(hostPort[1])
			if err != nil {
				log.Fatalf("Invalid port in address: %s", address)
			}

			nodes = append(nodes, raft.NodeConfig{
				ID:   raft.NodeID(peerID),
				Host: hostPort[0],
				Port: p,
			})
		}
	}

	config := raft.ClusterConfig{
		SelfID: raft.NodeID(*id),
		Nodes:  nodes,
	}

	transport := rpc.NewHTTPTransport()

	// Initialize the orchestrator
	raftNode, err := raft.NewRaftNode(config, raftLog, stateStore, wal, sm, transport, lastApplied)
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
