package api

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"raftkv/internal/auth"
	"raftkv/internal/config"
	"raftkv/internal/raft"
	"raftkv/internal/store"
)

type Server struct {
	node       *raft.RaftNode
	sm         *store.StateMachine
	config     raft.ClusterConfig
	proposeSem chan struct{}
	watchSem   chan struct{}
	identity   *auth.Identity
	tofu       *auth.TOFURegistry
}

func NewServer(node *raft.RaftNode, sm *store.StateMachine, clusterCfg raft.ClusterConfig, identity *auth.Identity, tofu *auth.TOFURegistry) *Server {
	return &Server{
		node:       node,
		sm:         sm,
		config:     clusterCfg,
		proposeSem: make(chan struct{}, config.MaxProposeConcurrency),
		watchSem:   make(chan struct{}, config.MaxConcurrentWatchers),
		identity:   identity,
		tofu:       tofu,
	}
}

func (s *Server) ListenAndServe(addr string) error {
	mux := http.NewServeMux()
	s.RegisterRoutes(mux)

	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{s.identity.Certificate},
		ClientAuth:   tls.RequestClientCert, // Request cert, but don't fail if CA is unknown
		// Bypass default verification to allow self-signed certs.
		// Security is instead handled by TOFU in VerifyConnection.
		VerifyPeerCertificate: func(rawCerts [][]byte, verifiedChains [][]*x509.Certificate) error {
			return nil
		},
		// Custom verification for TOFU
		VerifyConnection: func(cs tls.ConnectionState) error {
			if len(cs.PeerCertificates) == 0 {
				return nil // Might be a non-Raft client (like a browser/curl)
			}
			cert := cs.PeerCertificates[0]
			nodeID := cert.Subject.CommonName
			
			// We only enforce TOFU if the CommonName looks like a NodeID
			// For standard clients, they won't have a NodeID CommonName.
			return s.tofu.VerifyPeer(nodeID, cert)
		},
	}

	server := &http.Server{
		Addr:      addr,
		Handler:   mux,
		TLSConfig: tlsConfig,
	}

	log.Printf("[API] Starting SECURE server on %s (Fingerprint: %s)", addr, s.identity.Fingerprint)
	return server.ListenAndServeTLS("", "") // Certs are already in TLSConfig
}

func (s *Server) RegisterRoutes(mux *http.ServeMux) {
	// Client API routes
	mux.HandleFunc("GET /v1/keys/{key}", s.handleGet)
	mux.HandleFunc("PUT /v1/keys/{key}", s.handlePut)
	mux.HandleFunc("DELETE /v1/keys/{key}", s.handleDelete)
	mux.HandleFunc("GET /v1/keys", s.handleList)
	mux.HandleFunc("GET /v1/watch/{key}", s.handleWatchKey)
	mux.HandleFunc("GET /v1/watch", s.handleWatchNamespace)

	// Namespace-aware routes
	mux.HandleFunc("GET /v1/n/{ns}/keys/{key}", s.handleGet)
	mux.HandleFunc("PUT /v1/n/{ns}/keys/{key}", s.handlePut)
	mux.HandleFunc("DELETE /v1/n/{ns}/keys/{key}", s.handleDelete)
	mux.HandleFunc("GET /v1/n/{ns}/keys", s.handleList)
	mux.HandleFunc("DELETE /v1/n/{ns}", s.handleDeleteNamespace)
	mux.HandleFunc("GET /v1/n/{ns}/watch/{key}", s.handleWatchKey)
	mux.HandleFunc("GET /v1/n/{ns}/watch", s.handleWatchNamespace)

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
			// Switch to https for redirection
			leaderAddr := fmt.Sprintf("https://%s:%d%s", n.Host, n.Port, r.URL.Path)
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
		return config.DefaultNamespace
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
	index, _ := s.node.ProposeCommand(cmdBytes)

	if index == 0 {
		s.redirectToLeader(w, r)
		return
	}

	// Wait for the command to be committed
	timeout := time.After(config.WriteCommitTimeout)
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
	index, _ := s.node.ProposeCommand(cmdBytes)

	if index == 0 {
		s.redirectToLeader(w, r)
		return
	}

	timeout := time.After(config.WriteCommitTimeout)
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

func (s *Server) handleDeleteNamespace(w http.ResponseWriter, r *http.Request) {
	if s.redirectToLeader(w, r) {
		return
	}

	ns := s.getNamespace(r)
	if ns == config.DefaultNamespace {
		http.Error(w, "Cannot delete the default namespace", http.StatusForbidden)
		return
	}

	cmd := store.Command{
		Type:      store.CommandDeleteNamespace,
		Namespace: ns,
	}

	cmdBytes, err := json.Marshal(cmd)
	if err != nil {
		http.Error(w, "Failed to marshal command", http.StatusInternalServerError)
		return
	}

	s.proposeSem <- struct{}{}
	defer func() { <-s.proposeSem }()
	index, _ := s.node.ProposeCommand(cmdBytes)

	if index == 0 {
		s.redirectToLeader(w, r)
		return
	}

	timeout := time.After(config.WriteCommitTimeout)
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
	keys := s.sm.Keys(ns)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string][]string{"keys": keys})
}

func (s *Server) handleWatchKey(w http.ResponseWriter, r *http.Request) {
	if s.redirectToLeader(w, r) {
		return
	}

	ns := s.getNamespace(r)
	key := r.PathValue("key")
	if key == "" {
		http.Error(w, "Key is required", http.StatusBadRequest)
		return
	}

	select {
	case s.watchSem <- struct{}{}:
		defer func() { <-s.watchSem }()
	default:
		http.Error(w, "Too many concurrent watchers", http.StatusTooManyRequests)
		return
	}

	s.serveWatch(w, r, ns, key, true)
}

func (s *Server) handleWatchNamespace(w http.ResponseWriter, r *http.Request) {
	if s.redirectToLeader(w, r) {
		return
	}

	ns := s.getNamespace(r)

	select {
	case s.watchSem <- struct{}{}:
		defer func() { <-s.watchSem }()
	default:
		http.Error(w, "Too many concurrent watchers", http.StatusTooManyRequests)
		return
	}

	s.serveWatch(w, r, ns, "", false)
}

func (s *Server) serveWatch(w http.ResponseWriter, r *http.Request, ns, key string, isKeyWatch bool) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}

	var ch chan store.WatchEvent
	if isKeyWatch {
		ch = s.sm.Watchers.SubscribeKey(ns, key)
		defer s.sm.Watchers.UnsubscribeKey(ns, key, ch)
	} else {
		ch = s.sm.Watchers.SubscribeNamespace(ns)
		defer s.sm.Watchers.UnsubscribeNamespace(ns, ch)
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	fmt.Fprintf(w, "data: {\"status\": \"connected\", \"namespace\": \"%s\"}\n\n", ns)
	flusher.Flush()

	for {
		select {
		case <-r.Context().Done():
			return
		case event := <-ch:
			data, _ := json.Marshal(event)
			fmt.Fprintf(w, "data: %s\n\n", string(data))
			flusher.Flush()
		}
	}
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
