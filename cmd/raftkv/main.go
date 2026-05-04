package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"

	"raftkv/internal/api"
	"raftkv/internal/auth"
	"raftkv/internal/config"
	"raftkv/internal/persistence"
	"raftkv/internal/raft"
	"raftkv/internal/rpc"
	"raftkv/internal/store"
	"raftkv/internal/telemetry"
)

func main() {
	id := flag.String("id", "node1", "Node ID")
	port := flag.Int("port", 3001, "HTTP server port")
	dataDir := flag.String("data", "data", "Data directory")
	peersFlag := flag.String("peers", "", "Comma-separated list of peers (e.g. node2=localhost:3002,node3=localhost:3003)")
	envFlag := flag.String("env", "dev", "Environment (dev, staging, prod)")
	flag.Parse()

	// Ensure data directory exists
	if err := os.MkdirAll(*dataDir, 0755); err != nil {
		log.Fatalf("Failed to create data directory: %v", err)
	}

	// Load configuration
	cfg, err := config.Load(*envFlag)
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}
	log.Printf("Loaded environment: %s", cfg.Environment)
	
	// Override port if PORT environment variable is set
	if envPort := os.Getenv("PORT"); envPort != "" {
		if p, err := strconv.Atoi(envPort); err == nil {
			*port = p
		}
	}

	// TOFU mTLS Initialization
	identity, err := auth.LoadOrGenerateIdentity(*dataDir, *id)
	if err != nil {
		log.Fatalf("Failed to load/generate identity: %v", err)
	}

	tofu, err := auth.NewTOFURegistry(*dataDir)
	if err != nil {
		log.Fatalf("Failed to initialize TOFU registry: %v", err)
	}

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

	// Initialize SnapshotStore
	snapshotStore, err := persistence.NewSnapshotStore(*dataDir)
	if err != nil {
		log.Fatalf("Failed to initialize SnapshotStore: %v", err)
	}

	// Recover from snapshot and WAL
	raftLog := raft.NewRaftLog()
	var lastApplied raft.Index = 0

	log.Printf("Recovering from snapshot...")
	snapshot, err := snapshotStore.LoadSnapshot()
	if err != nil {
		log.Fatalf("Failed to load snapshot: %v", err)
	}

	if snapshot != nil {
		sm.Restore(snapshot.Data)
		lastApplied = raft.Index(snapshot.LastIncludedIndex)
		raftLog.SetBaseIndex(lastApplied, raft.Term(snapshot.LastIncludedTerm))
		log.Printf("Loaded snapshot through index %d term %d", snapshot.LastIncludedIndex, snapshot.LastIncludedTerm)
	}

	log.Printf("Recovering from WAL...")
	records, err := wal.ReadAll()
	if err != nil {
		log.Fatalf("Failed to read WAL entries: %v", err)
	}

	var entries []raft.LogEntry
	for _, record := range records {
		var entry raft.LogEntry
		if err := json.Unmarshal(record, &entry); err != nil {
			log.Fatalf("Failed to unmarshal log entry: %v", err)
		}
		if entry.Index <= lastApplied {
			continue
		}
		entries = append(entries, entry)
	}
	raftLog.Append(entries)
	log.Printf("Recovered %d entries into RaftLog. Last index: %d", len(entries), raftLog.LastIndex())

	// Replay committed entries into State Machine
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

	// Initialize the secure transport
	transport := rpc.NewHTTPTransport(tofu, identity)

	// Initialize Tracer with Neo4j support
	tracer, err := telemetry.NewTracer(*id, cfg.Neo4j.URI, cfg.Neo4j.Username, cfg.Neo4j.Password)
	if err != nil {
		log.Printf("Warning: Failed to initialize tracer: %v", err)
	}

	// Initialize the orchestrator
	raftNode, err := raft.NewRaftNode(config, raftLog, stateStore, wal, snapshotStore, sm, transport, lastApplied, tracer)
	if err != nil {
		log.Fatalf("Failed to initialize RaftNode: %v", err)
	}

	raftNode.Start() // starts the background election timer

	// Initialize the Secure HTTP API
	apiServer := api.NewServer(raftNode, sm, config, identity, tofu)

	addr := fmt.Sprintf(":%d", *port)

	// Handle graceful shutdown
	go func() {
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
		<-sigChan
		log.Printf("Shutting down...")
		raftNode.Stop()
		os.Exit(0)
	}()

	if err := apiServer.ListenAndServe(addr); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}
