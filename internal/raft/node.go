package raft

import (
	"encoding/json"
	"log"
	"math/rand"
	"sync"
	"time"

	"raftkv/internal/persistence"
	"raftkv/internal/store"
)

// RaftNode is the multi-threaded orchestrator that wraps the pure RaftCore.
// It handles timers, locks, network RPCs, and physical disk writes.
type RaftNode struct {
	mu            sync.Mutex
	core          *RaftCore
	stateStore    *persistence.StateStore
	wal           *persistence.WAL
	raftLog       *RaftLog
	stateMachine  *store.StateMachine
	transport     Transport
	lastApplied   Index

	electionTimer   *time.Timer
	heartbeatTicker *time.Ticker
	stopChan        chan struct{}
}

// NewRaftNode creates a new RaftNode orchestrator.
func NewRaftNode(
	config ClusterConfig,
	raftLog *RaftLog,
	stateStore *persistence.StateStore,
	wal *persistence.WAL,
	stateMachine *store.StateMachine,
	transport Transport,
	lastApplied Index,
) (*RaftNode, error) {
	// Load persisted state (if any)
	term, votedFor, err := stateStore.Load()
	if err != nil {
		return nil, err
	}

	persisted := PersistentState{
		CurrentTerm: Term(term),
		VotedFor:    NodeID(votedFor),
	}

	core := NewRaftCore(config, raftLog, persisted)

	return &RaftNode{
		core:         core,
		stateStore:   stateStore,
		wal:          wal,
		raftLog:      raftLog,
		stateMachine: stateMachine,
		transport:    transport,
		lastApplied:  lastApplied,
		stopChan:     make(chan struct{}),
	}, nil
}

// Start begins the background event loops (like the election timer).
func (n *RaftNode) Start() {
	n.mu.Lock()
	defer n.mu.Unlock()

	n.resetElectionTimerLocked()
}

// Stop shuts down the node.
func (n *RaftNode) Stop() {
	close(n.stopChan)
	if n.electionTimer != nil {
		n.electionTimer.Stop()
	}
}

// State returns the current volatile state of the node (safe for concurrent use).
func (n *RaftNode) State() NodeState {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.core.State()
}

// CurrentTerm returns the current term (safe for concurrent use).
func (n *RaftNode) CurrentTerm() Term {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.core.CurrentTerm()
}

// LeaderID returns the current leader ID if known (safe for concurrent use).
func (n *RaftNode) LeaderID() NodeID {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.core.LeaderID()
}

// executeActions runs the physical side effects returned by the pure RaftCore.
// Must be called with n.mu held.
func (n *RaftNode) executeActions(actions []Action) {
	for _, action := range actions {
		switch action.Type {
		case ActionPersistState:
			// Write CurrentTerm and VotedFor to state.json
			if err := n.stateStore.Save(uint64(action.State.CurrentTerm), string(action.State.VotedFor)); err != nil {
				log.Printf("[RaftNode] ERROR: Failed to persist state: %v", err)
			}
		
		case ActionAppendLog:
			if action.TruncateIndex > 0 {
				if err := n.wal.TruncateFromIndex(uint64(action.TruncateIndex)); err != nil {
					log.Printf("[RaftNode] ERROR: WAL TruncateFromIndex failed: %v", err)
				}
				n.raftLog.TruncateFrom(action.TruncateIndex)
			}
			for _, entry := range action.LogEntries {
				data, err := json.Marshal(entry)
				if err != nil {
					log.Printf("[RaftNode] ERROR: Failed to marshal LogEntry: %v", err)
					continue
				}
				if err := n.wal.Append(data); err != nil {
					log.Printf("[RaftNode] ERROR: WAL Append failed: %v", err)
				}
			}
			if len(action.LogEntries) > 0 {
				n.raftLog.Append(action.LogEntries)
			}

		case ActionCommit:
			for n.lastApplied < action.CommitIndex {
				n.lastApplied++
				entry, ok := n.raftLog.GetEntry(n.lastApplied)
				if ok {
					var cmd store.Command
					if err := json.Unmarshal(entry.Command, &cmd); err != nil {
						log.Printf("[RaftNode] ERROR: Failed to unmarshal command at index %d: %v", n.lastApplied, err)
						continue
					}
					n.stateMachine.Apply(cmd)
					log.Printf("[RaftNode] APPLIED command at index %d: %+v", n.lastApplied, cmd)
				}
			}

		case ActionResetElectionTimer:
			n.resetElectionTimerLocked()

		case ActionSendRequestVote:
			peerAddress := n.core.config.GetPeerAddress(action.To)
			go func(peerID NodeID, addr string, args RequestVoteArgs) {
				log.Printf("[RaftNode] Sending RequestVote to %s at %s for Term %d", peerID, addr, args.Term)
				reply, err := n.transport.SendRequestVote(peerID, addr, args)
				if err != nil {
					log.Printf("[RaftNode] Failed to send RequestVote to %s: %v", peerID, err)
					return
				}
				n.HandleRequestVoteResponse(peerID, reply)
			}(action.To, peerAddress, *action.RequestVoteArgs)
		
		case ActionSendAppendEntries:
			peerAddress := n.core.config.GetPeerAddress(action.To)
			go func(peerID NodeID, addr string, args AppendEntriesArgs) {
				// Don't log heartbeats to avoid spam
				if len(args.Entries) > 0 {
					log.Printf("[RaftNode] Sending AppendEntries to %s at %s with %d entries", peerID, addr, len(args.Entries))
				}
				reply, err := n.transport.SendAppendEntries(peerID, addr, args)
				if err != nil {
					// Also don't spam errors for missing heartbeats
					return
				}
				n.HandleAppendEntriesResponse(peerID, reply, args)
			}(action.To, peerAddress, *action.AppendEntriesArgs)

		case ActionBecomeLeader:
			log.Printf("[RaftNode] Elected LEADER for term %d!", action.Term)
			n.core.SetLeader()
			n.startHeartbeatTickerLocked()
		}
	}
}

// startHeartbeatTickerLocked starts sending periodic heartbeats.
func (n *RaftNode) startHeartbeatTickerLocked() {
	if n.electionTimer != nil {
		n.electionTimer.Stop()
	}
	if n.heartbeatTicker != nil {
		n.heartbeatTicker.Stop()
	}

	// Send initial heartbeat immediately
	actions := n.core.HandleHeartbeatTick()
	n.executeActions(actions)

	n.heartbeatTicker = time.NewTicker(50 * time.Millisecond)
	go func() {
		for {
			select {
			case <-n.stopChan:
				return
			case <-n.heartbeatTicker.C:
				n.mu.Lock()
				if n.core.State() == Leader {
					actions := n.core.HandleHeartbeatTick()
					n.executeActions(actions)
				} else {
					n.heartbeatTicker.Stop()
					n.mu.Unlock()
					return // stop if we stepped down
				}
				n.mu.Unlock()
			}
		}
	}()
}

// resetElectionTimerLocked sets a new randomized timeout (150-300ms).
func (n *RaftNode) resetElectionTimerLocked() {
	if n.electionTimer != nil {
		n.electionTimer.Stop()
	}
	if n.heartbeatTicker != nil {
		n.heartbeatTicker.Stop()
		n.heartbeatTicker = nil
	}

	// Randomized timeout between 150ms and 300ms
	timeout := time.Duration(150+rand.Intn(150)) * time.Millisecond

	n.electionTimer = time.AfterFunc(timeout, func() {
		n.mu.Lock()
		defer n.mu.Unlock()

		select {
		case <-n.stopChan:
			return
		default:
		}

		log.Printf("[RaftNode] Election timeout reached. Triggering new election.")
		actions := n.core.HandleElectionTimeout()
		n.executeActions(actions)
	})
}

// --- Thread-Safe RPC Handlers ---

// HandleRequestVote safely delegates an incoming RPC to the core state machine.
func (n *RaftNode) HandleRequestVote(args RequestVoteArgs) RequestVoteReply {
	n.mu.Lock()
	defer n.mu.Unlock()

	reply, actions := n.core.HandleRequestVoteRequest(args)
	n.executeActions(actions)
	return reply
}

// HandleRequestVoteResponse safely delegates an RPC reply to the core state machine.
func (n *RaftNode) HandleRequestVoteResponse(peer NodeID, reply RequestVoteReply) {
	n.mu.Lock()
	defer n.mu.Unlock()

	actions := n.core.HandleRequestVoteResponse(peer, reply)
	n.executeActions(actions)
}

// HandleAppendEntriesRequest safely delegates an incoming RPC to the core.
func (n *RaftNode) HandleAppendEntriesRequest(args AppendEntriesArgs) AppendEntriesReply {
	n.mu.Lock()
	defer n.mu.Unlock()

	reply, actions := n.core.HandleAppendEntriesRequest(args)
	n.executeActions(actions)
	return reply
}

// HandleAppendEntriesResponse safely delegates an RPC reply to the core.
func (n *RaftNode) HandleAppendEntriesResponse(peer NodeID, reply AppendEntriesReply, args AppendEntriesArgs) {
	n.mu.Lock()
	defer n.mu.Unlock()

	actions := n.core.HandleAppendEntriesResponse(peer, reply, args)
	n.executeActions(actions)
}

// ProposeCommand safely submits a command to the pure state machine if leader.
func (n *RaftNode) ProposeCommand(cmd []byte) (Index, Term) {
	n.mu.Lock()
	defer n.mu.Unlock()

	idx, term, actions := n.core.ProposeCommand(cmd)
	n.executeActions(actions)
	return idx, term
}
