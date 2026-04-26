package api

import (
	"encoding/json"
	"log"
	"net/http"
	"raftkv/internal/raft"
	"raftkv/internal/store"
)

type Server struct {
	node *raft.RaftNode
	sm   *store.StateMachine
}

func NewServer(node *raft.RaftNode, sm *store.StateMachine) *Server {
	return &Server{node: node, sm: sm}
}

func (s *Server) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/keys/{key}", s.handleGet)
	mux.HandleFunc("PUT /v1/keys/{key}", s.handlePut)
	mux.HandleFunc("DELETE /v1/keys/{key}", s.handleDelete)
	mux.HandleFunc("GET /v1/keys", s.handleList)
}

func (s *Server) handleGet(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	if key == "" {
		http.Error(w, "Key is required", http.StatusBadRequest)
		return
	}

	log.Printf("[API] GET /v1/keys/%s", key)

	val, ok := s.sm.Get(key)
	if !ok {
		http.Error(w, "Key not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"value": val})
}

func (s *Server) handlePut(w http.ResponseWriter, r *http.Request) {
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
		Type:  store.CommandSet,
		Key:   key,
		Value: reqBody.Value,
	}

	cmdBytes, err := json.Marshal(cmd)
	if err != nil {
		http.Error(w, "Failed to marshal command", http.StatusInternalServerError)
		return
	}

	index, term := s.node.ProposeCommand(cmdBytes)
	if index == 0 {
		http.Error(w, "Not the leader", http.StatusTemporaryRedirect)
		return
	}

	log.Printf("[API] Proposed PUT %s = %s (Index=%d, Term=%d)", key, reqBody.Value, index, term)

	// Since we are async and it might not be committed yet, return 202 Accepted
	w.WriteHeader(http.StatusAccepted)
}

func (s *Server) handleDelete(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	if key == "" {
		http.Error(w, "Key is required", http.StatusBadRequest)
		return
	}

	cmd := store.Command{
		Type: store.CommandDelete,
		Key:  key,
	}

	cmdBytes, err := json.Marshal(cmd)
	if err != nil {
		http.Error(w, "Failed to marshal command", http.StatusInternalServerError)
		return
	}

	index, term := s.node.ProposeCommand(cmdBytes)
	if index == 0 {
		http.Error(w, "Not the leader", http.StatusTemporaryRedirect)
		return
	}

	log.Printf("[API] Proposed DELETE %s (Index=%d, Term=%d)", key, index, term)

	w.WriteHeader(http.StatusAccepted)
}

func (s *Server) handleList(w http.ResponseWriter, r *http.Request) {
	log.Printf("[API] GET /v1/keys")
	keys := s.sm.Keys()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string][]string{"keys": keys})
}
