package raft

// Transport is the interface for node-to-node network communication.
type Transport interface {
	SendRequestVote(peerID NodeID, peerAddress string, args RequestVoteArgs) (RequestVoteReply, error)
	SendAppendEntries(peerID NodeID, peerAddress string, args AppendEntriesArgs) (AppendEntriesReply, error)
}
