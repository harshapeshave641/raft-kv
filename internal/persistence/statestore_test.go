package persistence

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStateStore_SaveAndLoad(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStateStore(dir)
	if err != nil {
		t.Fatalf("failed to create state store: %v", err)
	}

	// Test Load on fresh store
	term, votedFor, err := store.Load()
	if err != nil {
		t.Fatalf("failed to load fresh state: %v", err)
	}
	if term != 0 || votedFor != "" {
		t.Errorf("expected fresh state 0 and '', got %d and %s", term, votedFor)
	}

	// Test Save
	err = store.Save(2, "node1")
	if err != nil {
		t.Fatalf("failed to save state: %v", err)
	}

	// Test Load
	term, votedFor, err = store.Load()
	if err != nil {
		t.Fatalf("failed to load state: %v", err)
	}
	if term != 2 || votedFor != "node1" {
		t.Errorf("expected state 2 and 'node1', got %d and %s", term, votedFor)
	}

	// Verify atomicity by checking if tmp file was cleaned up
	if _, err := os.Stat(filepath.Join(dir, "state.json.tmp")); !os.IsNotExist(err) {
		t.Errorf("expected tmp file to be removed")
	}
}
