package raft

import "sync"

type RaftLog struct {
    // mu protects all fields
    mu      sync.RWMutex
    entries []LogEntry
}

func NewRaftLog() *RaftLog {
    return &RaftLog{}
}

// LastIndex returns the index of the last entry.
// Returns 0 if log is empty.
func (l *RaftLog) LastIndex() Index {
    l.mu.RLock()
    defer l.mu.RUnlock()
    if len(l.entries) == 0 {
        return 0
    }
    return l.entries[len(l.entries)-1].Index
}

// LastTerm returns the term of the last entry.
// Returns 0 if log is empty.
func (l *RaftLog) LastTerm() Term {
    l.mu.RLock()
    defer l.mu.RUnlock()
    if len(l.entries) == 0 {
        return 0
    }
    return l.entries[len(l.entries)-1].Term
}

// TermAt returns the term of the entry at index.
// Returns 0 if index not found.
func (l *RaftLog) TermAt(index Index) Term {
    l.mu.RLock()
    defer l.mu.RUnlock()
    if index == 0 || len(l.entries) == 0 {
        return 0
    }
    // entries are 1-indexed and densely packed from index 1
    pos := index - 1
    if int(pos) >= len(l.entries) {
        return 0
    }
    return l.entries[pos].Term
}

// HasEntry returns true if log contains an entry at index with the given term.
func (l *RaftLog) HasEntry(index Index, term Term) bool {
    l.mu.RLock()
    defer l.mu.RUnlock()
    if index == 0 {
        return true // index 0 is the virtual empty base, always true
    }
    pos := index - 1
    if int(pos) >= len(l.entries) {
        return false
    }
    return l.entries[pos].Term == term
}

// GetEntry returns the entry at index.
func (l *RaftLog) GetEntry(index Index) (LogEntry, bool) {
    l.mu.RLock()
    defer l.mu.RUnlock()
    if index == 0 || len(l.entries) == 0 {
        return LogEntry{}, false
    }
    pos := index - 1
    if int(pos) >= len(l.entries) {
        return LogEntry{}, false
    }
    return l.entries[pos], true
}

// GetEntriesFrom returns all entries from index (inclusive).
// Returns a copy — caller must not modify.
func (l *RaftLog) GetEntriesFrom(index Index) []LogEntry {
    l.mu.RLock()
    defer l.mu.RUnlock()
    if index == 0 || len(l.entries) == 0 {
        return nil
    }
    pos := index - 1
    if int(pos) >= len(l.entries) {
        return nil
    }
    result := make([]LogEntry, len(l.entries)-int(pos))
    copy(result, l.entries[pos:])
    return result
}

// Length returns the number of entries in the log.
func (l *RaftLog) Length() int {
    l.mu.RLock()
    defer l.mu.RUnlock()
    return len(l.entries)
}

// Append adds entries to the log.
// Called by RaftNode AFTER persisting to WAL.
func (l *RaftLog) Append(entries []LogEntry) {
    l.mu.Lock()
    defer l.mu.Unlock()
    l.entries = append(l.entries, entries...)
}

// TruncateFrom removes all entries from index (inclusive) onwards.
// Used when follower detects conflicting entries from leader.
// Called by RaftNode AFTER persisting to WAL.
func (l *RaftLog) TruncateFrom(index Index) {
    l.mu.Lock()
    defer l.mu.Unlock()
    if index == 0 || len(l.entries) == 0 {
        return
    }
    pos := index - 1
    if int(pos) < len(l.entries) {
        l.entries = l.entries[:pos]
    }
}