package persistence

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"

	"raftkv/internal/raft"
)

// StateStore persists currentTerm and votedFor.
// These must survive crashes independently of the log.
type StateStore struct {
	// mu protects all writes
	mu   sync.Mutex
	path string
}

func NewStateStore(dataDir string) (*StateStore, error) {
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return nil, err
	}
	return &StateStore{
		path: filepath.Join(dataDir, "state.json"),
	}, nil
}

type raftMetadata struct {
	CurrentTerm raft.Term   `json:"current_term"`
	VotedFor    raft.NodeID `json:"voted_for"`
}

// Save writes currentTerm and votedFor to disk atomically.
// Uses write-to-temp + fsync + rename — same pattern as your WAL.
// NEVER returns without fsyncing.
func (s *StateStore) Save(term raft.Term, votedFor raft.NodeID) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := json.Marshal(raftMetadata{
		CurrentTerm: term,
		VotedFor:    votedFor,
	})
	if err != nil {
		return err
	}

	// Write to temp file first
	tmpPath := s.path + ".tmp"
	tmp, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return err
	}

	// fsync before rename
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return err
	}
	tmp.Close()

	// Atomic rename
	return os.Rename(tmpPath, s.path)
}

// Load reads currentTerm and votedFor from disk.
// Returns zero values if file doesn't exist (fresh node).
func (s *StateStore) Load() (term raft.Term, votedFor raft.NodeID, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := os.ReadFile(s.path)
	if os.IsNotExist(err) {
		return 0, "", nil // fresh node
	}
	if err != nil {
		return 0, "", err
	}

	var meta raftMetadata
	if err := json.Unmarshal(data, &meta); err != nil {
		return 0, "", err
	}

	return meta.CurrentTerm, meta.VotedFor, nil
}
