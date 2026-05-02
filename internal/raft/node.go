package raft

import (
	"encoding/json"
	"log"
	"math/rand"
	"sync"
	"time"

	"raftkv/internal/persistence"
	"raftkv/internal/store"
	"raftkv/internal/telemetry"
	"github.com/google/uuid"
)

const snapshotThreshold = 200

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
	snapshotStore *persistence.SnapshotStore
	lastApplied   Index
	snapshotIndex Index
	tracer        *telemetry.Tracer

	electionTimer   *time.Timer
	heartbeatTicker *time.Ticker
	stopChan        chan struct{}

	lastTimerResetLog time.Time // To throttle noise
	
	// Tracking for telemetry
	prevState NodeState
	prevTerm  Term
}

// NewRaftNode creates a new RaftNode orchestrator.
func NewRaftNode(
	config ClusterConfig,
	raftLog *RaftLog,
	stateStore *persistence.StateStore,
	wal *persistence.WAL,
	snapshotStore *persistence.SnapshotStore,
	stateMachine *store.StateMachine,
	transport Transport,
	lastApplied Index,
	tracer *telemetry.Tracer,
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
		core:          core,
		stateStore:    stateStore,
		wal:           wal,
		raftLog:       raftLog,
		stateMachine:  stateMachine,
		transport:     transport,
		snapshotStore: snapshotStore,
		lastApplied:   lastApplied,
		snapshotIndex: lastApplied,
		tracer:        tracer,
		stopChan:      make(chan struct{}),
		prevState:     Follower,
		prevTerm:      Term(term),
	}, nil
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

// CommitIndex returns the current commit index (safe for concurrent use).
func (n *RaftNode) CommitIndex() Index {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.core.commitIndex
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
	if n.tracer != nil {
		n.tracer.Close()
	}
}

// executeActions runs the physical side effects returned by the pure RaftCore.
// Must be called with n.mu held.
func (n *RaftNode) executeActions(actions []Action) {
	// Detect and log state transitions
	currState := n.core.State()
	currTerm := n.core.CurrentTerm()

	if currState != n.prevState {
		n.tracer.Record(telemetry.EventStateTransition, uint64(currTerm), 0, map[string]interface{}{
			"from": n.prevState.String(),
			"to":   currState.String(),
		})
		n.prevState = currState
	}
	if currTerm > n.prevTerm {
		n.prevTerm = currTerm
		// Handled by ActionPersistState mostly, but we ensure it's tracked
	}

	for _, action := range actions {
		switch action.Type {
		case ActionPersistState:
			if err := n.stateStore.Save(uint64(action.State.CurrentTerm), string(action.State.VotedFor)); err != nil {
				log.Printf("[RaftNode] ERROR: Failed to persist state: %v", err)
			}
			n.tracer.Record(telemetry.EventTermChange, uint64(action.State.CurrentTerm), 0, map[string]interface{}{"voted_for": action.State.VotedFor})
		
		case ActionAppendLog:
			if action.TruncateIndex > 0 {
				if err := n.wal.TruncateFromIndex(uint64(action.TruncateIndex)); err != nil {
					log.Printf("[RaftNode] ERROR: WAL TruncateFromIndex failed: %v", err)
				}
				n.raftLog.TruncateFrom(action.TruncateIndex)
				n.tracer.Record(telemetry.EventTruncateLog, uint64(n.core.CurrentTerm()), uint64(action.TruncateIndex), nil)
			}
			for _, entry := range action.LogEntries {
				data, _ := json.Marshal(entry)
				n.wal.Append(data)
			}
			if len(action.LogEntries) > 0 {
				n.raftLog.Append(action.LogEntries)
				for _, entry := range action.LogEntries {
					n.tracer.Record(telemetry.EventAppendLog, uint64(entry.Term), uint64(entry.Index), nil)
				}
			}

		case ActionCommit:
			for n.lastApplied < action.CommitIndex {
				n.lastApplied++
				entry, ok := n.raftLog.GetEntry(n.lastApplied)
				if ok {
					var cmd store.Command
					json.Unmarshal(entry.Command, &cmd)
					n.stateMachine.Apply(cmd)
					n.tracer.Record(telemetry.EventCommit, uint64(entry.Term), uint64(n.lastApplied), map[string]interface{}{"command": cmd})
				}
			}
			n.maybeCreateSnapshotLocked()

		case ActionResetElectionTimer:
			n.resetElectionTimerLocked()

		case ActionSendRequestVote:
			peerAddress := n.core.config.GetPeerAddress(action.To)
			traceID := uuid.New().String()
			go func(peerID NodeID, addr string, args RequestVoteArgs, tid string) {
				start := time.Now()
				n.tracer.Record(telemetry.EventRPCSent, uint64(args.Term), 0, map[string]interface{}{
					"to": peerID, "rpc": "RequestVote", "trace_id": tid,
				})
				reply, err := n.transport.SendRequestVote(peerID, addr, args)
				latency := time.Since(start).Milliseconds()
				if err != nil {
					return
				}
				n.tracer.Record(telemetry.EventRPCReceived, uint64(reply.Term), 0, map[string]interface{}{
					"from": peerID, "rpc": "RequestVoteResponse", "granted": reply.VoteGranted, "latency_ms": latency, "trace_id": tid,
				})
				n.HandleRequestVoteResponse(peerID, reply)
			}(action.To, peerAddress, *action.RequestVoteArgs, traceID)
		
		case ActionSendAppendEntries:
			peerAddress := n.core.config.GetPeerAddress(action.To)
			traceID := uuid.New().String()
			go func(peerID NodeID, addr string, args AppendEntriesArgs, tid string) {
				start := time.Now()
				rpcType := "AppendEntries"
				if len(args.Entries) == 0 {
					rpcType = "Heartbeat"
				}
				n.tracer.Record(telemetry.EventRPCSent, uint64(args.Term), 0, map[string]interface{}{
					"to": peerID, "rpc": rpcType, "trace_id": tid,
				})
				reply, err := n.transport.SendAppendEntries(peerID, addr, args)
				latency := time.Since(start).Milliseconds()
				if err != nil {
					return
				}
				n.tracer.Record(telemetry.EventRPCReceived, uint64(reply.Term), 0, map[string]interface{}{
					"from": peerID, "rpc": rpcType + "Response", "success": reply.Success, "latency_ms": latency, "trace_id": tid,
				})
				n.HandleAppendEntriesResponse(peerID, reply, args)
			}(action.To, peerAddress, *action.AppendEntriesArgs, traceID)

		case ActionBecomeLeader:
			n.tracer.Record(telemetry.EventBecomeLeader, uint64(action.Term), uint64(n.raftLog.LastIndex()), nil)
			n.core.SetLeader()
			n.startHeartbeatTickerLocked()
		}
	}
}

func (n *RaftNode) maybeCreateSnapshotLocked() {
	if n.snapshotStore == nil || n.lastApplied == 0 || n.lastApplied-n.snapshotIndex < snapshotThreshold {
		return
	}
	snapshot := n.stateMachine.Snapshot()
	lastTerm := n.raftLog.TermAt(n.lastApplied)
	n.snapshotStore.SaveSnapshot(uint64(n.lastApplied), uint64(lastTerm), snapshot)
	n.wal.DiscardBeforeIndex(uint64(n.lastApplied + 1))
	n.raftLog.SetBaseIndex(n.lastApplied, lastTerm)
	n.snapshotIndex = n.lastApplied
	n.tracer.Record(telemetry.EventSnapshot, uint64(lastTerm), uint64(n.lastApplied), nil)
}

func (n *RaftNode) startHeartbeatTickerLocked() {
	if n.electionTimer != nil { n.electionTimer.Stop() }
	if n.heartbeatTicker != nil { n.heartbeatTicker.Stop() }
	actions := n.core.HandleHeartbeatTick()
	n.executeActions(actions)

	ticker := time.NewTicker(50 * time.Millisecond)
	n.heartbeatTicker = ticker
	go func(t *time.Ticker) {
		for {
			select {
			case <-n.stopChan:
				t.Stop()
				return
			case <-t.C:
				n.mu.Lock()
				if n.core.State() == Leader && n.heartbeatTicker == t {
					actions := n.core.HandleHeartbeatTick()
					n.executeActions(actions)
				} else {
					t.Stop(); n.mu.Unlock(); return
				}
				n.mu.Unlock()
			}
		}
	}(ticker)
}

func (n *RaftNode) resetElectionTimerLocked() {
	if n.electionTimer != nil { n.electionTimer.Stop() }
	if n.heartbeatTicker != nil { n.heartbeatTicker.Stop(); n.heartbeatTicker = nil }
	timeout := time.Duration(150+rand.Intn(150)) * time.Millisecond
	n.electionTimer = time.AfterFunc(timeout, func() {
		n.mu.Lock(); defer n.mu.Unlock()
		select {
		case <-n.stopChan: return
		default:
		}
		n.tracer.Record(telemetry.EventElectionTimeout, uint64(n.core.CurrentTerm()), 0, nil)
		actions := n.core.HandleElectionTimeout()
		n.executeActions(actions)
	})
}

func (n *RaftNode) HandleRequestVote(args RequestVoteArgs) RequestVoteReply {
	n.mu.Lock(); defer n.mu.Unlock()
	reply, actions := n.core.HandleRequestVoteRequest(args)
	n.tracer.Record(telemetry.EventRPCReceived, uint64(args.Term), 0, map[string]interface{}{
		"from": args.CandidateID, "rpc": "RequestVote", "granted": reply.VoteGranted,
	})
	n.executeActions(actions)
	return reply
}

func (n *RaftNode) HandleRequestVoteResponse(peer NodeID, reply RequestVoteReply) {
	n.mu.Lock(); defer n.mu.Unlock()
	actions := n.core.HandleRequestVoteResponse(peer, reply)
	n.executeActions(actions)
}

func (n *RaftNode) HandleAppendEntriesRequest(args AppendEntriesArgs) AppendEntriesReply {
	n.mu.Lock(); defer n.mu.Unlock()
	reply, actions := n.core.HandleAppendEntriesRequest(args)
	rpcType := "AppendEntries"
	if len(args.Entries) == 0 { rpcType = "Heartbeat" }
	n.tracer.Record(telemetry.EventRPCReceived, uint64(args.Term), 0, map[string]interface{}{
		"from": args.LeaderID, "rpc": rpcType, "success": reply.Success,
	})
	n.executeActions(actions)
	return reply
}

func (n *RaftNode) HandleAppendEntriesResponse(peer NodeID, reply AppendEntriesReply, args AppendEntriesArgs) {
	n.mu.Lock(); defer n.mu.Unlock()
	actions := n.core.HandleAppendEntriesResponse(peer, reply, args)
	n.executeActions(actions)
}

func (n *RaftNode) ProposeCommand(cmd []byte) (Index, Term) {
	n.mu.Lock(); defer n.mu.Unlock()
	idx, term, actions := n.core.ProposeCommand(cmd)
	n.executeActions(actions)
	return idx, term
}

func (s NodeState) String() string {
	switch s {
	case Follower: return "Follower"
	case Candidate: return "Candidate"
	case Leader: return "Leader"
	default: return "Unknown"
	}
}
