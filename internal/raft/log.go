package raft

import "sync"

type RaftLog struct {
    // mu protects all fields
    mu        sync.RWMutex
    entries   []LogEntry
    baseIndex Index
    baseTerm  Term
}

func NewRaftLog() *RaftLog {
    return &RaftLog{}
}

// LastIndex returns the index of the last entry, or the base index if the log is empty.
func (l *RaftLog) LastIndex() Index {
    l.mu.RLock()
    defer l.mu.RUnlock()
    if len(l.entries) == 0 {
        return l.baseIndex
    }
    return l.entries[len(l.entries)-1].Index
}

// LastTerm returns the term of the last entry, or the base term if the log is empty.
func (l *RaftLog) LastTerm() Term {
    l.mu.RLock()
    defer l.mu.RUnlock()
    if len(l.entries) == 0 {
        return l.baseTerm
    }
    return l.entries[len(l.entries)-1].Term
}

// TermAt returns the term of the entry at index.
// Returns 0 if index not found.
func (l *RaftLog) TermAt(index Index) Term {
    l.mu.RLock()
    defer l.mu.RUnlock()
    if index == 0 {
        return 0
    }
    if index == l.baseIndex {
        return l.baseTerm
    }
    if index < l.baseIndex {
        return 0
    }
    pos := index - l.baseIndex - 1
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
    if index == l.baseIndex {
        return term == l.baseTerm
    }
    if index < l.baseIndex {
        return false
    }
    pos := index - l.baseIndex - 1
    if int(pos) >= len(l.entries) {
        return false
    }
    return l.entries[pos].Term == term
}

// GetEntry returns the entry at index.
func (l *RaftLog) GetEntry(index Index) (LogEntry, bool) {
    l.mu.RLock()
    defer l.mu.RUnlock()
    if index == 0 || index <= l.baseIndex || len(l.entries) == 0 {
        return LogEntry{}, false
    }
    pos := index - l.baseIndex - 1
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

    if index <= l.baseIndex {
        return nil
    }

    if index > l.LastIndex() {
        return nil
    }

    pos := index - l.baseIndex - 1
    if pos >= Index(len(l.entries)) {
        return nil
    }

    count := Index(len(l.entries)) - pos
    result := make([]LogEntry, count)
    copy(result, l.entries[pos:])
    return result
}

// Length returns the number of entries in the log.
func (l *RaftLog) Length() int {
    l.mu.RLock()
    defer l.mu.RUnlock()
    return len(l.entries)
}

// BaseIndex returns the log's snapshot base index.
func (l *RaftLog) BaseIndex() Index {
    l.mu.RLock()
    defer l.mu.RUnlock()
    return l.baseIndex
}

// BaseTerm returns the term at the log's snapshot base index.
func (l *RaftLog) BaseTerm() Term {
    l.mu.RLock()
    defer l.mu.RUnlock()
    return l.baseTerm
}

// SetBaseIndex configures the base index and term after loading a snapshot.
// Also removes entries up to and including the base index.
func (l *RaftLog) SetBaseIndex(index Index, term Term) {
    l.mu.Lock()
    defer l.mu.Unlock()
    if index < l.baseIndex {
        return
    }
    
    // Remove entries that are now covered by the snapshot
    // Keep only entries after index (using OLD baseIndex for calculation)
    if len(l.entries) > 0 && index > 0 {
        // pos points to the first entry AFTER index (relative to OLD baseIndex)
        pos := int(index - l.baseIndex)
        if pos < len(l.entries) {
            l.entries = l.entries[pos:]
        } else {
            l.entries = nil
        }
    }
    
    // Now update baseIndex and baseTerm AFTER removing entries
    l.baseIndex = index
    l.baseTerm = term
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
    if index <= l.baseIndex {
        l.entries = nil
        return
    }
    if len(l.entries) == 0 {
        return
    }
    pos := int(index - l.baseIndex - 1)
    if pos < 0 {
        return
    }
    if pos < len(l.entries) {
        l.entries = l.entries[:pos]
    }
}

// DiscardEntriesBefore removes all entries before (and including) index.
// Updates baseIndex and baseTerm to reflect the snapshot boundary.
// Called by RaftNode after creating a snapshot to sync log state with snapshot.
func (l *RaftLog) DiscardEntriesBefore(upToIndex Index, upToTerm Term) {
    l.mu.Lock()
    defer l.mu.Unlock()
    if upToIndex <= l.baseIndex {
        return
    }
    // Find entries after upToIndex
    pos := int(upToIndex - l.baseIndex)
    if pos >= len(l.entries) {
        // All entries are before upToIndex, discard them all
        l.baseIndex = upToIndex
        l.baseTerm = upToTerm
        l.entries = nil
    } else {
        // Keep entries after upToIndex
        l.entries = l.entries[pos:]
        l.baseIndex = upToIndex
        l.baseTerm = upToTerm
    }
}
