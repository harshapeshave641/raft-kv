package rpc

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"raftkv/internal/raft"
)

type HTTPTransport struct {
	client *http.Client
}

func NewHTTPTransport() *HTTPTransport {
	return &HTTPTransport{
		client: &http.Client{
			Timeout: 100 * time.Millisecond, // Fast timeout for Raft
		},
	}
}

func (t *HTTPTransport) SendRequestVote(peerID raft.NodeID, peerAddress string, args raft.RequestVoteArgs) (raft.RequestVoteReply, error) {
	data, err := json.Marshal(args)
	if err != nil {
		return raft.RequestVoteReply{}, err
	}

	url := fmt.Sprintf("http://%s/raft/request-vote", peerAddress)
	resp, err := t.client.Post(url, "application/json", bytes.NewReader(data))
	if err != nil {
		return raft.RequestVoteReply{}, err
	}
	defer resp.Body.Close()

	var reply raft.RequestVoteReply
	if err := json.NewDecoder(resp.Body).Decode(&reply); err != nil {
		return raft.RequestVoteReply{}, err
	}
	return reply, nil
}

func (t *HTTPTransport) SendAppendEntries(peerID raft.NodeID, peerAddress string, args raft.AppendEntriesArgs) (raft.AppendEntriesReply, error) {
	data, err := json.Marshal(args)
	if err != nil {
		return raft.AppendEntriesReply{}, err
	}

	url := fmt.Sprintf("http://%s/raft/append-entries", peerAddress)
	resp, err := t.client.Post(url, "application/json", bytes.NewReader(data))
	if err != nil {
		return raft.AppendEntriesReply{}, err
	}
	defer resp.Body.Close()

	var reply raft.AppendEntriesReply
	if err := json.NewDecoder(resp.Body).Decode(&reply); err != nil {
		return raft.AppendEntriesReply{}, err
	}
	return reply, nil
}
