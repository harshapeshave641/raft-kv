package raft

// NodeID uniquely identifies a node in the Raft cluster.
type NodeID string

// Term represents a Raft election term.
type Term uint64

// Index represents a log index.
type Index uint64

// LogEntry represents a single entry in the Raft log.
type LogEntry struct {
	Term    Term   `json:"term"`
	Index   Index  `json:"index"`
	Command []byte `json:"command"` // Encoded state machine command
}

// PersistentState contains the Raft state that must be saved to stable storage 
// before responding to RPCs (as per the Raft paper).
type PersistentState struct {
	CurrentTerm Term       `json:"currentTerm"`
	VotedFor    NodeID     `json:"votedFor"`
	Log         []LogEntry `json:"log"`
}
