package rpc

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"raftkv/internal/auth"
	"raftkv/internal/raft"
)

type HTTPTransport struct {
	client *http.Client
	tofu   *auth.TOFURegistry
}

func NewHTTPTransport(tofu *auth.TOFURegistry, identity *auth.Identity) *HTTPTransport {
	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{identity.Certificate},
		// We use InsecureSkipVerify because we are using self-signed certs
		// and we will verify the fingerprint in the round tripper or handler.
		InsecureSkipVerify: true,
		VerifyConnection: func(cs tls.ConnectionState) error {
			if len(cs.PeerCertificates) == 0 {
				return fmt.Errorf("no peer certificates")
			}
			cert := cs.PeerCertificates[0]
			nodeID := cert.Subject.CommonName
			return tofu.VerifyPeer(nodeID, cert)
		},
	}

	return &HTTPTransport{
		client: &http.Client{
			Timeout: 200 * time.Millisecond,
			Transport: &http.Transport{
				TLSClientConfig: tlsConfig,
			},
		},
		tofu: tofu,
	}
}

func (t *HTTPTransport) SendRequestVote(peerID raft.NodeID, peerAddress string, args raft.RequestVoteArgs) (raft.RequestVoteReply, error) {
	data, err := json.Marshal(args)
	if err != nil {
		return raft.RequestVoteReply{}, err
	}

	url := fmt.Sprintf("https://%s/raft/request-vote", peerAddress)
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

	url := fmt.Sprintf("https://%s/raft/append-entries", peerAddress)
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
