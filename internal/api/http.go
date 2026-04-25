package api

import (
	"encoding/json"
	"net/http"
	"raftkv/internal/persistence"
	"raftkv/internal/raft"
	"raftkv/internal/store"
	"sync/atomic"
)

type Server struct {
	sm        *store.StateMachine
	wal       *persistence.WAL
	lastIndex *atomic.Uint64
}

func NewServer(sm *store.StateMachine, wal *persistence.WAL, lastIndex *atomic.Uint64) *Server {
	return &Server{sm: sm, wal: wal, lastIndex: lastIndex}
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

	index := s.lastIndex.Add(1)
	entry := raft.LogEntry{
		Term:    1,
		Index:   raft.Index(index),
		Command: cmdBytes,
	}

	if err := s.wal.AppendEntry(entry); err != nil {
		http.Error(w, "Failed to append to WAL", http.StatusInternalServerError)
		return
	}

	res := s.sm.Apply(cmd)

	if res.Error != "" {
		http.Error(w, res.Error, http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
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

	index := s.lastIndex.Add(1)
	entry := raft.LogEntry{
		Term:    1,
		Index:   raft.Index(index),
		Command: cmdBytes,
	}

	if err := s.wal.AppendEntry(entry); err != nil {
		http.Error(w, "Failed to append to WAL", http.StatusInternalServerError)
		return
	}

	res := s.sm.Apply(cmd)

	if res.Error != "" {
		http.Error(w, res.Error, http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleList(w http.ResponseWriter, r *http.Request) {
	keys := s.sm.Keys()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string][]string{"keys": keys})
}
