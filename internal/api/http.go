package api

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"
	"raftkv/internal/raft"
	"raftkv/internal/store"
)

type Server struct {
	node       *raft.RaftNode
	sm         *store.StateMachine
	config     raft.ClusterConfig
	proposeSem chan struct{}
}

func NewServer(node *raft.RaftNode, sm *store.StateMachine, config raft.ClusterConfig) *Server {
	return &Server{
		node:       node,
		sm:         sm,
		config:     config,
		proposeSem: make(chan struct{}, 5),
	}
}

func (s *Server) RegisterRoutes(mux *http.ServeMux) {
	// Client API routes (Backward compatibility for "default" namespace)
	mux.HandleFunc("GET /v1/keys/{key}", s.handleGet)
	mux.HandleFunc("PUT /v1/keys/{key}", s.handlePut)
	mux.HandleFunc("DELETE /v1/keys/{key}", s.handleDelete)
	mux.HandleFunc("GET /v1/keys", s.handleList)

	// Namespace-aware routes
	mux.HandleFunc("GET /v1/n/{ns}/keys/{key}", s.handleGet)
	mux.HandleFunc("PUT /v1/n/{ns}/keys/{key}", s.handlePut)
	mux.HandleFunc("DELETE /v1/n/{ns}/keys/{key}", s.handleDelete)
	mux.HandleFunc("GET /v1/n/{ns}/keys", s.handleList)

	// Internal Raft RPC routes
	mux.HandleFunc("POST /raft/request-vote", s.handleRequestVote)
	mux.HandleFunc("POST /raft/append-entries", s.handleAppendEntries)
}

func (s *Server) redirectToLeader(w http.ResponseWriter, r *http.Request) bool {
	if s.node.State() == raft.Leader {
		return false
	}
	leaderID := s.node.LeaderID()
	for _, n := range s.config.Nodes {
		if n.ID == leaderID {
			leaderAddr := fmt.Sprintf("http://%s:%d%s", n.Host, n.Port, r.URL.Path)
			w.Header().Set("Location", leaderAddr)
			w.WriteHeader(http.StatusTemporaryRedirect)
			return true
		}
	}
	http.Error(w, "Not the leader", http.StatusTemporaryRedirect)
	return true
}

func (s *Server) getNamespace(r *http.Request) string {
	ns := r.PathValue("ns")
	if ns == "" {
		return store.DefaultNamespace
	}
	return ns
}

func (s *Server) handleGet(w http.ResponseWriter, r *http.Request) {
	if s.redirectToLeader(w, r) {
		return
	}

	ns := s.getNamespace(r)
	key := r.PathValue("key")
	if key == "" {
		http.Error(w, "Key is required", http.StatusBadRequest)
		return
	}

	log.Printf("[API] GET /v1/%s/keys/%s", ns, key)
	
	val, ok := s.sm.Get(ns, key)
	if !ok {
		http.Error(w, "Key not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"value": val})
}

func (s *Server) handlePut(w http.ResponseWriter, r *http.Request) {
	if s.redirectToLeader(w, r) {
		return
	}

	ns := s.getNamespace(r)
	key := r.PathValue("key")
	if key == "" {
		http.Error(w, "Key is required", http.StatusBadRequest)
		return
	}

	var reqBody struct {
		Value string `json:"value"`
	}

	if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	cmd := store.Command{
		Type:      store.CommandSet,
		Namespace: ns,
		Key:       key,
		Value:     reqBody.Value,
	}

	cmdBytes, err := json.Marshal(cmd)
	if err != nil {
		http.Error(w, "Failed to marshal command", http.StatusInternalServerError)
		return
	}

	s.proposeSem <- struct{}{}
	defer func() { <-s.proposeSem }()
	index, term := s.node.ProposeCommand(cmdBytes)

	if index == 0 {
		s.redirectToLeader(w, r)
		return
	}

	log.Printf("[API] Proposed PUT %s/%s = %s (Index=%d, Term=%d)", ns, key, reqBody.Value, index, term)

	// Wait for the command to be committed before returning success
	timeout := time.After(5 * time.Second)
	ticker := time.NewTicker(1 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-timeout:
			http.Error(w, "Command not committed within timeout", http.StatusRequestTimeout)
			return
		case <-ticker.C:
			if s.redirectToLeader(w, r) {
				return
			}
			if s.node.CommitIndex() >= index {
				w.WriteHeader(http.StatusOK)
				return
			}
		}
	}
}

func (s *Server) handleDelete(w http.ResponseWriter, r *http.Request) {
	if s.redirectToLeader(w, r) {
		return
	}

	ns := s.getNamespace(r)
	key := r.PathValue("key")
	if key == "" {
		http.Error(w, "Key is required", http.StatusBadRequest)
		return
	}

	cmd := store.Command{
		Type:      store.CommandDelete,
		Namespace: ns,
		Key:       key,
	}

	cmdBytes, err := json.Marshal(cmd)
	if err != nil {
		http.Error(w, "Failed to marshal command", http.StatusInternalServerError)
		return
	}

	s.proposeSem <- struct{}{}
	defer func() { <-s.proposeSem }()
	index, term := s.node.ProposeCommand(cmdBytes)

	if index == 0 {
		s.redirectToLeader(w, r)
		return
	}

	log.Printf("[API] Proposed DELETE %s/%s (Index=%d, Term=%d)", ns, key, index, term)

	// Wait for the command to be committed before returning success
	timeout := time.After(5 * time.Second)
	ticker := time.NewTicker(1 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-timeout:
			http.Error(w, "Command not committed within timeout", http.StatusRequestTimeout)
			return
		case <-ticker.C:
			if s.redirectToLeader(w, r) {
				return
			}
			if s.node.CommitIndex() >= index {
				w.WriteHeader(http.StatusOK)
				return
			}
		}
	}
}

func (s *Server) handleList(w http.ResponseWriter, r *http.Request) {
	if s.redirectToLeader(w, r) {
		return
	}

	ns := s.getNamespace(r)
	log.Printf("[API] LIST /v1/%s/keys", ns)

	keys := s.sm.Keys(ns)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string][]string{"keys": keys})
}

func (s *Server) handleRequestVote(w http.ResponseWriter, r *http.Request) {
	var args raft.RequestVoteArgs
	if err := json.NewDecoder(r.Body).Decode(&args); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	reply := s.node.HandleRequestVote(args)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(reply)
}

func (s *Server) handleAppendEntries(w http.ResponseWriter, r *http.Request) {
	var args raft.AppendEntriesArgs
	if err := json.NewDecoder(r.Body).Decode(&args); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	reply := s.node.HandleAppendEntriesRequest(args)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(reply)
}
