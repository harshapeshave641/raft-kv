package raft

// RaftCore is a pure state machine.
// Zero goroutines. Zero I/O. Zero network calls.
// Every method takes input, returns []Action.
// RaftNode executes the actions.
type RaftCore struct {
    selfID NodeID
    config ClusterConfig
    log    LogStore

    // Persistent state — reflected in ActionPersistState before every response
    currentTerm Term
    votedFor    NodeID

    // Volatile state on all servers
    commitIndex   Index
    state         NodeState
    leaderID      NodeID
    votesReceived map[NodeID]bool

    // Volatile state on leaders
    nextIndex  map[NodeID]Index
    matchIndex map[NodeID]Index
}

func NewRaftCore(
    config    ClusterConfig,
    log       LogStore,
    persisted PersistentState,
) *RaftCore {
    return &RaftCore{
        selfID:      config.SelfID,
        config:      config,
        log:         log,
        currentTerm: persisted.CurrentTerm,
        votedFor:    persisted.VotedFor,
        state:       Follower,
        votesReceived: make(map[NodeID]bool),
        commitIndex:   0,
    }
}

// --- Read-only accessors ---

func (c *RaftCore) State() NodeState  { return c.state }
func (c *RaftCore) CurrentTerm() Term { return c.currentTerm }
func (c *RaftCore) VotedFor() NodeID  { return c.votedFor }
func (c *RaftCore) LeaderID() NodeID  { return c.leaderID }
func (c *RaftCore) IsLeader() bool    { return c.state == Leader }
func (c *RaftCore) SelfID() NodeID    { return c.selfID }

// --- State transitions ---

// HandleElectionTimeout is called when the election timer fires.
// Follower or Candidate → starts election.
// Leader → ignores.
func (c *RaftCore) HandleElectionTimeout() []Action {
    if c.state == Leader {
        return nil // leaders don't start elections
    }

    // Become candidate
    c.state       = Candidate
    c.currentTerm++
    c.votedFor    = c.selfID
    c.votesReceived = make(map[NodeID]bool)
    c.votesReceived[c.selfID] = true

    actions := []Action{
        // Persist FIRST — before anything else
        {
            Type: ActionPersistState,
            State: &PersistentState{
                CurrentTerm: c.currentTerm,
                VotedFor:    c.votedFor,
            },
        },
        {
            Type: ActionResetElectionTimer,
        },
    }

    // Single node cluster — quorum is 1, win immediately
    if c.config.Quorum() == 1 {
        actions = append(actions, Action{
            Type: ActionBecomeLeader,
            Term: c.currentTerm,
        })
        return actions
    }

    // Multi-node — send RequestVote to all peers
    // (implemented in next baby step)
    for _, peer := range c.config.Peers() {
        actions = append(actions, Action{
            Type: ActionSendRequestVote,
            To:   peer.ID,
            RequestVoteArgs: &RequestVoteArgs{
                Term:         c.currentTerm,
                CandidateID:  c.selfID,
                LastLogIndex: c.log.LastIndex(),
                LastLogTerm:  c.log.LastTerm(),
            },
        })
    }

    return actions
}

// SetLeader is called by RaftNode when executing ActionBecomeLeader.
func (c *RaftCore) SetLeader() {
    c.state    = Leader
    c.leaderID = c.selfID
    c.nextIndex = make(map[NodeID]Index)
    c.matchIndex = make(map[NodeID]Index)
    
    lastIndex := c.log.LastIndex()
    for _, peer := range c.config.Peers() {
        c.nextIndex[peer.ID] = lastIndex + 1
        c.matchIndex[peer.ID] = 0
    }
}

// StepDown reverts node to follower with the given term.
// Called when a higher term is seen in any message.
func (c *RaftCore) StepDown(term Term) []Action {
    c.state       = Follower
    c.currentTerm = term
    c.votedFor    = ""
    c.leaderID    = ""
    c.votesReceived = nil

    return []Action{
        {
            Type: ActionPersistState,
            State: &PersistentState{
                CurrentTerm: c.currentTerm,
                VotedFor:    c.votedFor,
            },
        },
        {
            Type: ActionResetElectionTimer,
        },
    }
}

// HandleRequestVoteRequest handles incoming RequestVote RPCs.
func (c *RaftCore) HandleRequestVoteRequest(args RequestVoteArgs) (RequestVoteReply, []Action) {
	var actions []Action
	reply := RequestVoteReply{
		Term:        c.currentTerm,
		VoteGranted: false,
	}

	if args.Term < c.currentTerm {
		return reply, nil
	}

	steppedDown := false
	if args.Term > c.currentTerm {
		c.state = Follower
		c.currentTerm = args.Term
		c.votedFor = ""
		c.leaderID = ""
		c.votesReceived = nil
		steppedDown = true
		reply.Term = c.currentTerm
	}

	// Check log up-to-date
	lastLogTerm := c.log.LastTerm()
	lastLogIndex := c.log.LastIndex()
	logIsUpToDate := (args.LastLogTerm > lastLogTerm) ||
		(args.LastLogTerm == lastLogTerm && args.LastLogIndex >= lastLogIndex)

	if (c.votedFor == "" || c.votedFor == args.CandidateID) && logIsUpToDate {
		c.votedFor = args.CandidateID
		reply.VoteGranted = true
		
		actions = append(actions, Action{
			Type: ActionPersistState,
			State: &PersistentState{
				CurrentTerm: c.currentTerm,
				VotedFor:    c.votedFor,
			},
		}, Action{
			Type: ActionResetElectionTimer,
		})
	} else if steppedDown {
		// Only persist the stepdown if we didn't grant the vote (which persists itself)
		actions = append(actions, Action{
			Type: ActionPersistState,
			State: &PersistentState{
				CurrentTerm: c.currentTerm,
				VotedFor:    c.votedFor,
			},
		}, Action{
			Type: ActionResetElectionTimer,
		})
	}

	return reply, actions
}

// HandleRequestVoteResponse handles the response from a peer to our RequestVote RPC.
func (c *RaftCore) HandleRequestVoteResponse(peer NodeID, reply RequestVoteReply) []Action {
	if c.state != Candidate {
		return nil // We don't care about votes if we aren't a candidate
	}

	if reply.Term > c.currentTerm {
		return c.StepDown(reply.Term)
	}

	if reply.Term == c.currentTerm && reply.VoteGranted {
		c.votesReceived[peer] = true
		
		if len(c.votesReceived) >= c.config.Quorum() {
			return []Action{{
				Type: ActionBecomeLeader,
				Term: c.currentTerm,
			}}
		}
	}

	return nil
}

// ProposeCommand takes a client command and initiates replication if leader.
func (c *RaftCore) ProposeCommand(cmd []byte) (Index, Term, []Action) {
	if c.state != Leader {
		return 0, 0, nil // Clients must retry with actual leader
	}

	entry := LogEntry{
		Term:    c.currentTerm,
		Index:   c.log.LastIndex() + 1,
		Command: cmd,
	}

	actions := []Action{
		{
			Type:          ActionAppendLog,
			TruncateIndex: entry.Index,
			LogEntries:    []LogEntry{entry},
		},
	}

	for _, peer := range c.config.Peers() {
		prevLogIndex := c.nextIndex[peer.ID] - 1
		actions = append(actions, Action{
			Type: ActionSendAppendEntries,
			To:   peer.ID,
			AppendEntriesArgs: &AppendEntriesArgs{
				Term:         c.currentTerm,
				LeaderID:     c.selfID,
				PrevLogIndex: prevLogIndex,
				PrevLogTerm:  c.log.TermAt(prevLogIndex),
				Entries:      []LogEntry{entry},
				LeaderCommit: c.commitIndex,
			},
		})
	}

	// Single node cluster: commit immediately
	if c.config.Quorum() == 1 {
		c.commitIndex = entry.Index
		actions = append(actions, Action{
			Type:        ActionCommit,
			CommitIndex: c.commitIndex,
		})
	}

	return entry.Index, entry.Term, actions
}

// HandleHeartbeatTick is called periodically by the leader to send AppendEntries (heartbeats).
func (c *RaftCore) HandleHeartbeatTick() []Action {
	if c.state != Leader {
		return nil
	}

	var actions []Action
	for _, peer := range c.config.Peers() {
		prevLogIndex := c.nextIndex[peer.ID] - 1
		actions = append(actions, Action{
			Type: ActionSendAppendEntries,
			To:   peer.ID,
			AppendEntriesArgs: &AppendEntriesArgs{
				Term:         c.currentTerm,
				LeaderID:     c.selfID,
				PrevLogIndex: prevLogIndex,
				PrevLogTerm:  c.log.TermAt(prevLogIndex),
				Entries:      c.log.GetEntriesFrom(c.nextIndex[peer.ID]),
				LeaderCommit: c.commitIndex,
			},
		})
	}
	return actions
}

// HandleAppendEntriesRequest handles replication and heartbeats from leader.
func (c *RaftCore) HandleAppendEntriesRequest(args AppendEntriesArgs) (AppendEntriesReply, []Action) {
	var actions []Action
	reply := AppendEntriesReply{
		Term:    c.currentTerm,
		Success: false,
	}

	if args.Term < c.currentTerm {
		return reply, nil
	}

	if args.Term > c.currentTerm || c.state != Follower {
		c.state = Follower
		c.currentTerm = args.Term
		c.votedFor = ""
		c.leaderID = ""
		c.votesReceived = nil
		actions = append(actions, Action{
			Type: ActionPersistState,
			State: &PersistentState{
				CurrentTerm: c.currentTerm,
				VotedFor:    c.votedFor,
			},
		})
	}
	c.leaderID = args.LeaderID
	reply.Term = c.currentTerm

	actions = append(actions, Action{
		Type: ActionResetElectionTimer,
	})

	if args.PrevLogIndex > 0 {
		termAtPrev := c.log.TermAt(args.PrevLogIndex)
		if termAtPrev != args.PrevLogTerm {
			return reply, actions
		}
	}

	insertIndex := args.PrevLogIndex + 1
	newEntriesIndex := 0

	for {
		if newEntriesIndex >= len(args.Entries) {
			break
		}
		entry := args.Entries[newEntriesIndex]
		existingTerm := c.log.TermAt(insertIndex)
		if existingTerm == 0 {
			break // Reached end of local log
		}
		if existingTerm != entry.Term {
			break // Conflict!
		}
		insertIndex++
		newEntriesIndex++
	}

	if newEntriesIndex < len(args.Entries) {
		actions = append(actions, Action{
			Type:          ActionAppendLog,
			TruncateIndex: insertIndex,
			LogEntries:    args.Entries[newEntriesIndex:],
		})
	}

	reply.Success = true

	if args.LeaderCommit > c.commitIndex {
		lastNewEntryIndex := args.PrevLogIndex + Index(len(args.Entries))
		if args.LeaderCommit < lastNewEntryIndex {
			c.commitIndex = args.LeaderCommit
		} else {
			c.commitIndex = lastNewEntryIndex
		}
		actions = append(actions, Action{
			Type:        ActionCommit,
			CommitIndex: c.commitIndex,
		})
	}

	return reply, actions
}

// HandleAppendEntriesResponse tracks replication success and advances commitIndex.
func (c *RaftCore) HandleAppendEntriesResponse(peer NodeID, reply AppendEntriesReply, args AppendEntriesArgs) []Action {
	if c.state != Leader {
		return nil
	}

	if reply.Term > c.currentTerm {
		return c.StepDown(reply.Term)
	}

	var actions []Action

	if reply.Success {
		match := args.PrevLogIndex + Index(len(args.Entries))
		if match > c.matchIndex[peer] {
			c.matchIndex[peer] = match
		}
		c.nextIndex[peer] = c.matchIndex[peer] + 1

		// Check if we can commit
		// We can commit index N if a majority have matchIndex >= N, and log[N].term == currentTerm
		for N := c.log.LastIndex(); N > c.commitIndex; N-- {
			if c.log.TermAt(N) != c.currentTerm {
				continue // Raft paper: never commit log entries from previous terms by counting replicas
			}

			count := 1 // self
			for _, peerConfig := range c.config.Peers() {
				if c.matchIndex[peerConfig.ID] >= N {
					count++
				}
			}

			if count >= c.config.Quorum() {
				c.commitIndex = N
				actions = append(actions, Action{
					Type:        ActionCommit,
					CommitIndex: c.commitIndex,
				})
				break
			}
		}
	} else {
		// Log inconsistency: decrement nextIndex and retry
		c.nextIndex[peer]--
		if c.nextIndex[peer] < 1 {
			c.nextIndex[peer] = 1
		}
		
		prevLogIndex := c.nextIndex[peer] - 1
		actions = append(actions, Action{
			Type: ActionSendAppendEntries,
			To:   peer,
			AppendEntriesArgs: &AppendEntriesArgs{
				Term:         c.currentTerm,
				LeaderID:     c.selfID,
				PrevLogIndex: prevLogIndex,
				PrevLogTerm:  c.log.TermAt(prevLogIndex),
				Entries:      c.log.GetEntriesFrom(c.nextIndex[peer]),
				LeaderCommit: c.commitIndex,
			},
		})
	}

	return actions
}