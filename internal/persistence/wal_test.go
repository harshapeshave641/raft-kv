package persistence

import (
	"reflect"
	"testing"

	"raftkv/internal/raft"
)

func TestWAL_AppendAndReadAll(t *testing.T) {
	dir := t.TempDir()
	w, err := NewWAL(dir)
	if err != nil {
		t.Fatalf("failed to create WAL: %v", err)
	}

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

	w.Close()
}

func TestWAL_AppendAndReadEntries(t *testing.T) {
	dir := t.TempDir()
	w, err := NewWAL(dir)
	if err != nil {
		t.Fatalf("failed to create WAL: %v", err)
	}

	entries := []raft.LogEntry{
		{Term: 1, Index: 1, Command: []byte("set x 1")},
		{Term: 1, Index: 2, Command: []byte("set y 2")},
		{Term: 2, Index: 3, Command: []byte("delete x")},
	}

	for _, e := range entries {
		if err := w.AppendEntry(e); err != nil {
			t.Fatalf("failed to append entry: %v", err)
		}
	}

	loadedEntries, err := w.ReadEntries()
	if err != nil {
		t.Fatalf("failed to read entries: %v", err)
	}

	if !reflect.DeepEqual(entries, loadedEntries) {
		t.Errorf("loaded entries do not match saved entries.\nSaved: %+v\nLoaded: %+v", entries, loadedEntries)
	}

	w.Close()
}

func TestWAL_TruncateTo(t *testing.T) {
	dir := t.TempDir()
	w, err := NewWAL(dir)
	if err != nil {
		t.Fatalf("failed to create WAL: %v", err)
	}

	w.Append([]byte("record1"))
	w.Append([]byte("record2"))
	w.Append([]byte("record3"))

	if err := w.TruncateTo(2); err != nil {
		t.Fatalf("failed to truncate: %v", err)
	}

	records, err := w.ReadAll()
	if err != nil {
		t.Fatalf("failed to read all after truncate: %v", err)
	}

	if len(records) != 2 {
		t.Fatalf("expected 2 records, got %d", len(records))
	}

	if string(records[0]) != "record2" {
		t.Errorf("expected record2, got %s", string(records[0]))
	}

	if string(records[1]) != "record3" {
		t.Errorf("expected record3, got %s", string(records[1]))
	}

	w.Close()
}
