package persistence_test

import (
	"encoding/json"
	"raftkv/internal/persistence"
	"raftkv/internal/raft"
	"reflect"
	"testing"
)

func TestWAL_AppendAndReadAll(t *testing.T) {
	dir := t.TempDir()
	w, err := persistence.NewWAL(dir)
	if err != nil {
		t.Fatalf("failed to create WAL: %v", err)
	}
	defer w.Close()

	data1 := []byte("hello")
	data2 := []byte("world")

	if err := w.Append(data1); err != nil {
		t.Fatalf("failed to append data1: %v", err)
	}
	if err := w.Append(data2); err != nil {
		t.Fatalf("failed to append data2: %v", err)
	}

	records, err := w.ReadAll()
	if err != nil {
		t.Fatalf("failed to read all: %v", err)
	}

	if len(records) != 2 {
		t.Fatalf("expected 2 records, got %d", len(records))
	}

	if string(records[0]) != "hello" {
		t.Errorf("expected hello, got %s", string(records[0]))
	}

	if string(records[1]) != "world" {
		t.Errorf("expected world, got %s", string(records[1]))
	}
}

func TestWAL_AppendAndReadEntries(t *testing.T) {
	dir := t.TempDir()
	w, err := persistence.NewWAL(dir)
	if err != nil {
		t.Fatalf("failed to create WAL: %v", err)
	}
	defer w.Close()

	entries := []raft.LogEntry{
		{Term: 1, Index: 1, Command: []byte("set x 1")},
		{Term: 1, Index: 2, Command: []byte("set y 2")},
		{Term: 2, Index: 3, Command: []byte("delete x")},
	}

	for _, e := range entries {
		data, _ := json.Marshal(e)
		if err := w.Append(data); err != nil {
			t.Fatalf("failed to append entry: %v", err)
		}
	}

	records, err := w.ReadAll()
	if err != nil {
		t.Fatalf("failed to read records: %v", err)
	}

	var loadedEntries []raft.LogEntry
	for _, r := range records {
		var e raft.LogEntry
		json.Unmarshal(r, &e)
		loadedEntries = append(loadedEntries, e)
	}

	if !reflect.DeepEqual(entries, loadedEntries) {
		t.Errorf("loaded entries do not match saved entries.\nSaved: %+v\nLoaded: %+v", entries, loadedEntries)
	}
}

func TestWAL_DiscardBeforeIndex(t *testing.T) {
	dir := t.TempDir()
	w, err := persistence.NewWAL(dir)
	if err != nil {
		t.Fatalf("failed to create WAL: %v", err)
	}
	defer w.Close()

	entries := []raft.LogEntry{
		{Term: 1, Index: 1, Command: []byte("1")},
		{Term: 1, Index: 2, Command: []byte("2")},
		{Term: 1, Index: 3, Command: []byte("3")},
	}

	for _, e := range entries {
		data, _ := json.Marshal(e)
		w.Append(data)
	}

	// Discard entries before index 2 (keeps 2 and 3)
	if err := w.DiscardBeforeIndex(2); err != nil {
		t.Fatalf("failed to discard: %v", err)
	}

	records, err := w.ReadAll()
	if err != nil {
		t.Fatalf("failed to read all after discard: %v", err)
	}

	if len(records) != 2 {
		t.Fatalf("expected 2 records, got %d", len(records))
	}

	var e2 raft.LogEntry
	json.Unmarshal(records[0], &e2)
	if e2.Index != 2 {
		t.Errorf("expected index 2, got %d", e2.Index)
	}
}

func TestWAL_TruncateFromIndex(t *testing.T) {
	dir := t.TempDir()
	w, err := persistence.NewWAL(dir)
	if err != nil {
		t.Fatalf("failed to create WAL: %v", err)
	}
	defer w.Close()

	entries := []raft.LogEntry{
		{Term: 1, Index: 1, Command: []byte("1")},
		{Term: 1, Index: 2, Command: []byte("2")},
		{Term: 1, Index: 3, Command: []byte("3")},
	}

	for _, e := range entries {
		data, _ := json.Marshal(e)
		w.Append(data)
	}

	// Truncate from index 2 (keeps only 1)
	if err := w.TruncateFromIndex(2); err != nil {
		t.Fatalf("failed to truncate: %v", err)
	}

	records, err := w.ReadAll()
	if err != nil {
		t.Fatalf("failed to read all after truncate: %v", err)
	}

	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}

	var e1 raft.LogEntry
	json.Unmarshal(records[0], &e1)
	if e1.Index != 1 {
		t.Errorf("expected index 1, got %d", e1.Index)
	}
}
