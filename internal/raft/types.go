package raft

import "fmt"

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
}

type NodeState string

const (
	Follower  NodeState = "Follower"
	Candidate NodeState = "Candidate"
	Leader    NodeState = "Leader"
)

type ActionType string

const (
	ActionPersistState       ActionType = "PersistState"
	ActionResetElectionTimer ActionType = "ResetElectionTimer"
	ActionBecomeLeader       ActionType = "BecomeLeader"
	ActionSendRequestVote    ActionType = "SendRequestVote"
	ActionAppendLog          ActionType = "AppendLog"
	ActionCommit             ActionType = "Commit"
	ActionSendAppendEntries  ActionType = "SendAppendEntries"
)

type Action struct {
	Type              ActionType
	State             *PersistentState
	Term              Term
	To                NodeID
	RequestVoteArgs   *RequestVoteArgs
	AppendEntriesArgs *AppendEntriesArgs
	CommitIndex       Index
	TruncateIndex     Index
	LogEntries        []LogEntry
}

type NodeConfig struct {
	ID   NodeID
	Host string
	Port int
}

type ClusterConfig struct {
	SelfID NodeID
	Nodes  []NodeConfig
}

func (c *ClusterConfig) Quorum() int {
	return (len(c.Nodes) / 2) + 1
}

func (c *ClusterConfig) Peers() []NodeConfig {
	var peers []NodeConfig
	for _, n := range c.Nodes {
		if n.ID != c.SelfID {
			peers = append(peers, n)
		}
	}
	return peers
}

func (c *ClusterConfig) GetPeerAddress(id NodeID) string {
	for _, n := range c.Nodes {
		if n.ID == id {
			return n.Host + ":" + fmt.Sprint(n.Port)
		}
	}
	return ""
}

type RequestVoteArgs struct {
	Term         Term
	CandidateID  NodeID
	LastLogIndex Index
	LastLogTerm  Term
}

type RequestVoteReply struct {
	Term        Term
	VoteGranted bool
}

type AppendEntriesArgs struct {
	Term         Term
	LeaderID     NodeID
	PrevLogIndex Index
	PrevLogTerm  Term
	Entries      []LogEntry
	LeaderCommit Index
}

type AppendEntriesReply struct {
	Term    Term
	Success bool
}

type LogStore interface {
	LastIndex() Index
	LastTerm() Term
	TermAt(index Index) Term
	GetEntriesFrom(index Index) []LogEntry
}
