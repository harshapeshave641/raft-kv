package raft

import (
    "testing"
    
)

func singleNodeConfig() ClusterConfig {
    return ClusterConfig{
        SelfID: "node1",
        Nodes: []NodeConfig{
            {ID: "node1", Host: "localhost", Port: 3001},
        },
    }
}

func threeNodeConfig() ClusterConfig {
    return ClusterConfig{
        SelfID: "node1",
        Nodes: []NodeConfig{
            {ID: "node1", Host: "localhost", Port: 3001},
            {ID: "node2", Host: "localhost", Port: 3002},
            {ID: "node3", Host: "localhost", Port: 3003},
        },
    }
}

func newCore(config ClusterConfig) *RaftCore {
    return NewRaftCore(config, NewRaftLog(), PersistentState{})
}

// --- Initial state ---

func TestInitialStateIsFollower(t *testing.T) {
    c := newCore(singleNodeConfig())
    if c.State() != Follower {
        t.Errorf("expected Follower, got %s", c.State())
    }
}

func TestInitialTermIsZero(t *testing.T) {
    c := newCore(singleNodeConfig())
    if c.CurrentTerm() != 0 {
        t.Errorf("expected term 0, got %d", c.CurrentTerm())
    }
}

func TestInitialVotedForIsEmpty(t *testing.T) {
    c := newCore(singleNodeConfig())
    if c.VotedFor() != "" {
        t.Errorf("expected no vote, got %s", c.VotedFor())
    }
}

func TestInitialIsNotLeader(t *testing.T) {
    c := newCore(singleNodeConfig())
    if c.IsLeader() {
        t.Error("should not be leader on start")
    }
}

// --- Election timeout ---

func TestElectionTimeoutIncrementsTermByOne(t *testing.T) {
    c := newCore(singleNodeConfig())
    c.HandleElectionTimeout()
    if c.CurrentTerm() != 1 {
        t.Errorf("expected term 1, got %d", c.CurrentTerm())
    }
}

func TestElectionTimeoutVotesForSelf(t *testing.T) {
    c := newCore(singleNodeConfig())
    c.HandleElectionTimeout()
    if c.VotedFor() != "node1" {
        t.Errorf("expected votedFor node1, got %s", c.VotedFor())
    }
}

func TestElectionTimeoutTransitionsToCandidate(t *testing.T) {
    c := newCore(singleNodeConfig())
    c.HandleElectionTimeout()
    if c.State() != Candidate {
        t.Errorf("expected Candidate, got %s", c.State())
    }
}

// --- Actions ---

func TestFirstActionIsPersistState(t *testing.T) {
    c := newCore(singleNodeConfig())
    actions := c.HandleElectionTimeout()
    if len(actions) == 0 {
        t.Fatal("expected actions, got none")
    }
    if actions[0].Type != ActionPersistState {
        t.Errorf("first action must be PersistState, got %s", actions[0].Type)
    }
}

func TestPersistStateHasCorrectTerm(t *testing.T) {
    c := newCore(singleNodeConfig())
    actions := c.HandleElectionTimeout()
    if actions[0].State.CurrentTerm != 1 {
        t.Errorf("expected persisted term 1, got %d", actions[0].State.CurrentTerm)
    }
}

func TestPersistStateHasCorrectVotedFor(t *testing.T) {
    c := newCore(singleNodeConfig())
    actions := c.HandleElectionTimeout()
    if actions[0].State.VotedFor != "node1" {
        t.Errorf("expected persisted votedFor node1, got %s", actions[0].State.VotedFor)
    }
}

// --- Single node election ---

func TestSingleNodeGetsBecomeLeaderAction(t *testing.T) {
    c := newCore(singleNodeConfig())
    actions := c.HandleElectionTimeout()

    found := false
    for _, a := range actions {
        if a.Type == ActionBecomeLeader {
            found = true
            break
        }
    }
    if !found {
        t.Error("single node should get ActionBecomeLeader")
    }
}

func TestSingleNodeBecomesLeaderAfterSetLeader(t *testing.T) {
    c := newCore(singleNodeConfig())
    actions := c.HandleElectionTimeout()
    for _, a := range actions {
        if a.Type == ActionBecomeLeader {
            c.SetLeader()
        }
    }
    if !c.IsLeader() {
        t.Error("expected leader after SetLeader()")
    }
    if c.LeaderID() != "node1" {
        t.Errorf("expected leaderID node1, got %s", c.LeaderID())
    }
}

// --- Multi node election ---

func TestMultiNodeSendsRequestVoteToPeers(t *testing.T) {
    c := newCore(threeNodeConfig())
    actions := c.HandleElectionTimeout()

    votesSent := 0
    for _, a := range actions {
        if a.Type == ActionSendRequestVote {
            votesSent++
        }
    }
    if votesSent != 2 {
        t.Errorf("expected 2 RequestVote actions for 3-node cluster, got %d", votesSent)
    }
}

func TestMultiNodeDoesNotGetBecomeLeaderImmediately(t *testing.T) {
    c := newCore(threeNodeConfig())
    actions := c.HandleElectionTimeout()

    for _, a := range actions {
        if a.Type == ActionBecomeLeader {
            t.Error("multi-node should not get ActionBecomeLeader immediately")
        }
    }
}

// --- Leader ignores timeout ---

func TestLeaderIgnoresElectionTimeout(t *testing.T) {
    c := newCore(singleNodeConfig())
    actions := c.HandleElectionTimeout()
    for _, a := range actions {
        if a.Type == ActionBecomeLeader {
            c.SetLeader()
        }
    }

    actions2 := c.HandleElectionTimeout()
    if len(actions2) != 0 {
        t.Errorf("leader should return no actions on timeout, got %d", len(actions2))
    }
    if c.CurrentTerm() != 1 {
        t.Error("leader term should not change on timeout")
    }
}

// --- StepDown ---

func TestStepDownRevertsToFollower(t *testing.T) {
    c := newCore(singleNodeConfig())
    actions := c.HandleElectionTimeout()
    for _, a := range actions {
        if a.Type == ActionBecomeLeader {
            c.SetLeader()
        }
    }

    c.StepDown(5)

    if c.State() != Follower {
        t.Errorf("expected Follower after StepDown, got %s", c.State())
    }
    if c.CurrentTerm() != 5 {
        t.Errorf("expected term 5, got %d", c.CurrentTerm())
    }
    if c.VotedFor() != "" {
        t.Error("votedFor should be cleared on StepDown")
    }
    if c.LeaderID() != "" {
        t.Error("leaderID should be cleared on StepDown")
    }
}

func TestStepDownPersistsState(t *testing.T) {
    c := newCore(singleNodeConfig())
    actions := c.StepDown(3)

    if len(actions) == 0 || actions[0].Type != ActionPersistState {
        t.Error("StepDown must emit ActionPersistState as first action")
    }
    if actions[0].State.CurrentTerm != 3 {
        t.Errorf("expected persisted term 3, got %d", actions[0].State.CurrentTerm)
    }
}

// --- Recovered state ---

func TestCoreRestoresTermFromPersistentState(t *testing.T) {
    persisted := PersistentState{CurrentTerm: 7, VotedFor: "node2"}
    c := NewRaftCore(singleNodeConfig(), NewRaftLog(), persisted)

    if c.CurrentTerm() != 7 {
        t.Errorf("expected recovered term 7, got %d", c.CurrentTerm())
    }
    if c.VotedFor() != "node2" {
        t.Errorf("expected recovered votedFor node2, got %s", c.VotedFor())
    }
}

// --- RequestVote Request ---

func TestRequestVote_RejectOlderTerm(t *testing.T) {
	persisted := PersistentState{CurrentTerm: 5, VotedFor: "node2"}
	c := NewRaftCore(singleNodeConfig(), NewRaftLog(), persisted)

	args := RequestVoteArgs{
		Term:         4,
		CandidateID:  "node3",
		LastLogIndex: 10,
		LastLogTerm:  5,
	}

	reply, actions := c.HandleRequestVoteRequest(args)

	if reply.VoteGranted {
		t.Error("should reject vote for older term")
	}
	if len(actions) > 0 {
		t.Error("should not emit actions on old term rejection")
	}
}

func TestRequestVote_StepDownOnNewerTerm(t *testing.T) {
	c := newCore(singleNodeConfig())
	c.HandleElectionTimeout() // Becomes candidate, term 1
	c.SetLeader() // Becomes leader, term 1

	args := RequestVoteArgs{
		Term:         2,
		CandidateID:  "node2",
		LastLogIndex: 0,
		LastLogTerm:  0,
	}

	reply, actions := c.HandleRequestVoteRequest(args)

	if c.State() != Follower {
		t.Errorf("should step down to Follower, got %s", c.State())
	}
	if reply.Term != 2 {
		t.Errorf("reply term should be 2, got %d", reply.Term)
	}
	if !reply.VoteGranted {
		t.Error("should grant vote since logs are equal and we stepped down")
	}

	// Verify action
	if len(actions) != 2 {
		t.Fatalf("expected 2 actions (Persist, ResetTimer), got %d", len(actions))
	}
	if actions[0].Type != ActionPersistState {
		t.Errorf("expected ActionPersistState, got %s", actions[0].Type)
	}
	if actions[0].State.VotedFor != "node2" {
		t.Errorf("expected VotedFor node2, got %s", actions[0].State.VotedFor)
	}
}

func TestRequestVote_RejectIfAlreadyVotedForAnother(t *testing.T) {
	persisted := PersistentState{CurrentTerm: 5, VotedFor: "node2"}
	c := NewRaftCore(singleNodeConfig(), NewRaftLog(), persisted)

	args := RequestVoteArgs{
		Term:         5,
		CandidateID:  "node3",
		LastLogIndex: 10,
		LastLogTerm:  5,
	}

	reply, _ := c.HandleRequestVoteRequest(args)

	if reply.VoteGranted {
		t.Error("should reject vote if already voted for another in same term")
	}
}

func TestRequestVote_RejectIfLogIsOlder(t *testing.T) {
	l := NewRaftLog()
	l.Append([]LogEntry{{Term: 3, Index: 1, Command: []byte("cmd")}})
	c := NewRaftCore(singleNodeConfig(), l, PersistentState{})

	args := RequestVoteArgs{
		Term:         5,
		CandidateID:  "node2",
		LastLogIndex: 0,
		LastLogTerm:  0,
	}

	reply, _ := c.HandleRequestVoteRequest(args)

	if reply.VoteGranted {
		t.Error("should reject vote if candidate log is older")
	}
}

func TestRequestVote_GrantIfLogIsNewer(t *testing.T) {
	l := NewRaftLog()
	l.Append([]LogEntry{{Term: 3, Index: 1, Command: []byte("cmd")}})
	c := NewRaftCore(singleNodeConfig(), l, PersistentState{CurrentTerm: 3})

	args := RequestVoteArgs{
		Term:         5,
		CandidateID:  "node2",
		LastLogIndex: 2,
		LastLogTerm:  4,
	}

	reply, _ := c.HandleRequestVoteRequest(args)

	if !reply.VoteGranted {
		t.Error("should grant vote if candidate log is newer")
	}
}

// --- RequestVote Response ---

func TestRequestVoteResponse_IgnoreIfNotCandidate(t *testing.T) {
	c := newCore(threeNodeConfig())
	// Currently Follower

	actions := c.HandleRequestVoteResponse("node2", RequestVoteReply{
		Term:        0,
		VoteGranted: true,
	})

	if len(actions) > 0 {
		t.Error("should ignore vote responses if not candidate")
	}
}

func TestRequestVoteResponse_StepDownIfTermHigher(t *testing.T) {
	c := newCore(threeNodeConfig())
	c.HandleElectionTimeout() // term 1, Candidate

	actions := c.HandleRequestVoteResponse("node2", RequestVoteReply{
		Term:        2,
		VoteGranted: false,
	})

	if c.State() != Follower {
		t.Error("should step down if reply term is higher")
	}
	if c.CurrentTerm() != 2 {
		t.Errorf("should update term to 2, got %d", c.CurrentTerm())
	}
	if len(actions) == 0 || actions[0].Type != ActionPersistState {
		t.Error("should emit ActionPersistState on step down")
	}
}

func TestRequestVoteResponse_BecomesLeaderOnQuorum(t *testing.T) {
	c := newCore(threeNodeConfig())
	c.HandleElectionTimeout() // term 1, Candidate, voted for self (1 vote)

	actions := c.HandleRequestVoteResponse("node2", RequestVoteReply{
		Term:        1,
		VoteGranted: true,
	})

	// Quorum for 3 nodes is 2. We should now become leader.
	found := false
	for _, a := range actions {
		if a.Type == ActionBecomeLeader {
			found = true
			if a.Term != 1 {
				t.Errorf("ActionBecomeLeader term should be 1, got %d", a.Term)
			}
		}
	}
	if !found {
		t.Error("expected ActionBecomeLeader after reaching quorum")
	}
}

// --- AppendEntries Request ---

func TestAppendEntries_RejectOlderTerm(t *testing.T) {
	c := newCore(singleNodeConfig())
	c.HandleElectionTimeout() // term 1

	args := AppendEntriesArgs{
		Term:         0,
		LeaderID:     "node2",
		PrevLogIndex: 0,
		PrevLogTerm:  0,
	}

	reply, _ := c.HandleAppendEntriesRequest(args)
	if reply.Success {
		t.Error("should reject older term")
	}
}

func TestAppendEntries_StepDownOnHigherTerm(t *testing.T) {
	c := newCore(singleNodeConfig())
	c.HandleElectionTimeout() // Candidate, term 1
	c.SetLeader() // Leader, term 1

	args := AppendEntriesArgs{
		Term:         2,
		LeaderID:     "node2",
		PrevLogIndex: 0,
		PrevLogTerm:  0,
	}

	reply, actions := c.HandleAppendEntriesRequest(args)
	if c.State() != Follower {
		t.Error("should step down to Follower on higher term heartbeat")
	}
	if reply.Term != 2 {
		t.Errorf("expected reply term 2, got %d", reply.Term)
	}

	foundPersist := false
	for _, a := range actions {
		if a.Type == ActionPersistState {
			foundPersist = true
		}
	}
	if !foundPersist {
		t.Error("expected ActionPersistState on step down")
	}
}

func TestAppendEntries_RejectIfPrevLogMissing(t *testing.T) {
	c := newCore(singleNodeConfig())

	args := AppendEntriesArgs{
		Term:         1,
		LeaderID:     "node2",
		PrevLogIndex: 5,
		PrevLogTerm:  1,
	}

	reply, _ := c.HandleAppendEntriesRequest(args)
	if reply.Success {
		t.Error("should reject if prevLogIndex is missing")
	}
}

func TestAppendEntries_AppendNewEntriesAndAdvanceCommit(t *testing.T) {
	c := newCore(singleNodeConfig())

	args := AppendEntriesArgs{
		Term:         1,
		LeaderID:     "node2",
		PrevLogIndex: 0,
		PrevLogTerm:  0,
		Entries: []LogEntry{
			{Term: 1, Index: 1, Command: []byte("cmd1")},
		},
		LeaderCommit: 1,
	}

	reply, actions := c.HandleAppendEntriesRequest(args)
	if !reply.Success {
		t.Error("expected success")
	}

	foundAppend := false
	foundCommit := false
	for _, a := range actions {
		if a.Type == ActionAppendLog {
			foundAppend = true
			if a.TruncateIndex != 1 {
				t.Errorf("expected truncate index 1, got %d", a.TruncateIndex)
			}
		}
		if a.Type == ActionCommit {
			foundCommit = true
			if a.CommitIndex != 1 {
				t.Errorf("expected commit index 1, got %d", a.CommitIndex)
			}
		}
	}

	if !foundAppend || !foundCommit {
		t.Error("expected ActionAppendLog and ActionCommit")
	}
}

// --- ProposeCommand ---

func TestProposeCommand_FailsIfNotLeader(t *testing.T) {
	c := newCore(singleNodeConfig())
	index, _, actions := c.ProposeCommand([]byte("test"))
	if index != 0 || len(actions) > 0 {
		t.Error("Follower should reject ProposeCommand")
	}
}

func TestProposeCommand_LeaderAppendsAndBroadcasts(t *testing.T) {
	c := newCore(threeNodeConfig())
	c.HandleElectionTimeout()
	c.SetLeader() // term 1

	index, term, actions := c.ProposeCommand([]byte("test"))
	if index != 1 || term != 1 {
		t.Errorf("expected index 1 term 1, got %d %d", index, term)
	}

	foundAppend := false
	broadcasts := 0
	for _, a := range actions {
		if a.Type == ActionAppendLog {
			foundAppend = true
		}
		if a.Type == ActionSendAppendEntries {
			broadcasts++
			if len(a.AppendEntriesArgs.Entries) != 1 {
				t.Error("expected 1 entry in broadcast")
			}
		}
	}

	if !foundAppend {
		t.Error("expected ActionAppendLog")
	}
	if broadcasts != 2 {
		t.Errorf("expected 2 broadcasts for 3 node cluster, got %d", broadcasts)
	}
}

// --- AppendEntries Response ---

func TestAppendEntriesResponse_AdvancesCommitIndex(t *testing.T) {
	l := NewRaftLog()
	l.Append([]LogEntry{{Term: 1, Index: 1, Command: []byte("test")}})
	c := NewRaftCore(threeNodeConfig(), l, PersistentState{CurrentTerm: 1})
	c.SetLeader()

	// Pretend node2 replied success to our append of index 1
	reply := AppendEntriesReply{Term: 1, Success: true}
	args := AppendEntriesArgs{PrevLogIndex: 0, Entries: []LogEntry{{Term: 1, Index: 1, Command: []byte("test")}}}
	
	actions := c.HandleAppendEntriesResponse("node2", reply, args)

	foundCommit := false
	for _, a := range actions {
		if a.Type == ActionCommit {
			foundCommit = true
			if a.CommitIndex != 1 {
				t.Errorf("expected commit index 1, got %d", a.CommitIndex)
			}
		}
	}
	if !foundCommit {
		t.Error("expected ActionCommit after reaching quorum replication")
	}
}
