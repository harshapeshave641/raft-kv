package config

import "time"

const (
	SnapshotThreshold = 200

	HeartbeatInterval = 50 * time.Millisecond

	ElectionTimeoutMin = 150
	ElectionTimeoutMax = 300
)

const (
	MaxConcurrentWatchers = 50

	MaxProposeConcurrency = 5
	WriteCommitTimeout = 5 * time.Second
)

const (
	DefaultNamespace = "default"
)
