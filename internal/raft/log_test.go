package raft

import (
	"testing"
)

func TestRaftLog_AppendAndGet(t *testing.T) {
	l := NewRaftLog()

	if l.Length() != 0 {
		t.Fatalf("expected length 0, got %d", l.Length())
	}

	entries := []LogEntry{
		{Term: 1, Index: 1, Command: []byte("cmd1")},
		{Term: 1, Index: 2, Command: []byte("cmd2")},
	}

	l.Append(entries)

	if l.Length() != 2 {
		t.Fatalf("expected length 2, got %d", l.Length())
	}

	if l.LastIndex() != 2 {
		t.Errorf("expected last index 2, got %d", l.LastIndex())
	}

	if l.LastTerm() != 1 {
		t.Errorf("expected last term 1, got %d", l.LastTerm())
	}

	entry, ok := l.GetEntry(1)
	if !ok || entry.Term != 1 || string(entry.Command) != "cmd1" {
		t.Errorf("failed to get entry 1 correctly: %+v", entry)
	}

	if l.TermAt(2) != 1 {
		t.Errorf("expected term 1 at index 2")
	}

	if !l.HasEntry(2, 1) {
		t.Errorf("expected to have entry index 2 term 1")
	}
}

func TestRaftLog_TruncateFrom(t *testing.T) {
	l := NewRaftLog()
	l.Append([]LogEntry{
		{Term: 1, Index: 1},
		{Term: 1, Index: 2},
		{Term: 2, Index: 3},
		{Term: 2, Index: 4},
	})

	l.TruncateFrom(3)

	if l.Length() != 2 {
		t.Fatalf("expected length 2 after truncate, got %d", l.Length())
	}

	if l.LastIndex() != 2 {
		t.Errorf("expected last index 2, got %d", l.LastIndex())
	}

	entries := l.GetEntriesFrom(1)
	if len(entries) != 2 {
		t.Errorf("expected 2 entries from index 1, got %d", len(entries))
	}
}
