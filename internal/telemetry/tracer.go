package telemetry

import (
	"context"
	"encoding/json"
	"log"
	"sync"
	"time"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

type EventType string

const (
	EventStateTransition EventType = "StateTransition"
	EventTermChange      EventType = "TermChange"
	EventElectionStart   EventType = "ElectionStart"
	EventVoteReceived    EventType = "VoteReceived"
	EventVoteGranted     EventType = "VoteGranted"
	EventBecomeLeader    EventType = "BecomeLeader"
	EventAppendLog       EventType = "AppendLog"
	EventTruncateLog     EventType = "TruncateLog"
	EventCommit          EventType = "Commit"
	EventRPCSent         EventType = "RPCSent"
	EventRPCReceived     EventType = "RPCReceived"
	EventSnapshot        EventType = "Snapshot"
)

type Event struct {
	Timestamp time.Time              `json:"timestamp"`
	NodeID    string                 `json:"node_id"`
	Type      EventType              `json:"type"`
	Term      uint64                 `json:"term"`
	Index     uint64                 `json:"index,omitempty"`
	Details   map[string]interface{} `json:"details,omitempty"`
}

type Tracer struct {
	mu     sync.Mutex
	nodeID string

	// Neo4j fields
	driver   neo4j.DriverWithContext
	eventCh  chan Event
	stopChan chan struct{}
}

func NewTracer(nodeID string, neo4jURI, neo4jUser, neo4jPassword string) (*Tracer, error) {
	t := &Tracer{
		nodeID:   nodeID,
		eventCh:  make(chan Event, 10000), // Large buffer for high traffic
		stopChan: make(chan struct{}),
	}

	if neo4jURI != "" {
		driver, err := neo4j.NewDriverWithContext(neo4jURI, neo4j.BasicAuth(neo4jUser, neo4jPassword, ""))
		if err != nil {
			log.Printf("[Tracer] Error: Failed to create Neo4j driver: %v", err)
		} else {
			// Verify connection
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := driver.VerifyConnectivity(ctx); err != nil {
				log.Printf("[Tracer] Error: Neo4j connectivity check failed: %v", err)
				driver.Close(ctx)
			} else {
				log.Printf("[Tracer] Successfully connected to Neo4j")
				t.driver = driver
				go t.neo4jWorker()
			}
		}
	}

	return t, nil
}

func (t *Tracer) Record(eventType EventType, term uint64, index uint64, details map[string]interface{}) {
	if t == nil || t.driver == nil {
		return
	}

	event := Event{
		Timestamp: time.Now(),
		NodeID:    t.nodeID,
		Type:      eventType,
		Term:      term,
		Index:     index,
		Details:   details,
	}

	// Send to Neo4j worker (non-blocking)
	select {
	case t.eventCh <- event:
	default:
		// Drop event if channel is full to protect Raft performance
	}
}

func (t *Tracer) neo4jWorker() {
	ctx := context.Background()
	log.Printf("[Tracer] Neo4j worker started for node %s", t.nodeID)
	for {
		select {
		case <-t.stopChan:
			log.Printf("[Tracer] Neo4j worker stopping...")
			return
		case event := <-t.eventCh:
			err := t.writeToNeo4j(ctx, event)
			if err != nil {
				log.Printf("[Tracer] Neo4j write error: %v", err)
			}
		}
	}
}

func (t *Tracer) writeToNeo4j(ctx context.Context, e Event) error {
	session := t.driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeWrite})
	defer session.Close(ctx)

	_, err := session.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) (interface{}, error) {
		detailsJSON, _ := json.Marshal(e.Details)
		detailsStr := string(detailsJSON)
		
		params := map[string]interface{}{
			"id":      e.NodeID,
			"term":    e.Term,
			"idx":     e.Index,
			"ts":      e.Timestamp.Format(time.RFC3339Nano),
			"details": detailsStr,
			"type":    string(e.Type),
		}

		// Always ensure Server and Term exist
		tx.Run(ctx, "MERGE (s:Server {id: $id})", params)
		tx.Run(ctx, "MERGE (t:Term {value: $term})", params)
		
		// Record the event node itself for history
		tx.Run(ctx, "MATCH (s:Server {id: $id}), (t:Term {value: $term}) CREATE (s)-[:LOGGED_EVENT {type: $type, timestamp: $ts, details: $details}]->(t)", params)

		switch e.Type {
		case EventTermChange:
			tx.Run(ctx, "MATCH (s:Server {id: $id}), (t:Term {value: $term}) MERGE (s)-[r:IN_TERM]->(t) SET r.timestamp = $ts", params)
			if v, ok := e.Details["voted_for"].(string); ok && v != "" {
				params["voted_for"] = v
				tx.Run(ctx, "MATCH (s:Server {id: $id}) MERGE (s2:Server {id: $voted_for}) MERGE (s)-[r:VOTED_FOR {term: $term}]->(s2) SET r.timestamp = $ts", params)
			}
		case EventBecomeLeader:
			tx.Run(ctx, "MATCH (s:Server {id: $id}), (t:Term {value: $term}) MERGE (s)-[r:LEADER_OF]->(t) SET r.timestamp = $ts", params)
		case EventAppendLog:
			tx.Run(ctx, "MATCH (s:Server {id: $id}) MERGE (e:LogEntry {index: $idx, term: $term}) MERGE (s)-[r:HAS_LOG_ENTRY]->(e) SET r.timestamp = $ts", params)
		case EventCommit:
			tx.Run(ctx, "MATCH (s:Server {id: $id}) MERGE (e:LogEntry {index: $idx, term: $term}) MERGE (s)-[r:APPLIED]->(e) SET r.timestamp = $ts, r.command = $details", params)
		case EventRPCSent:
			if to, ok := e.Details["to"].(string); ok {
				params["to"] = to
				params["rpc"] = e.Details["rpc"]
				tx.Run(ctx, "MATCH (s1:Server {id: $id}) MERGE (s2:Server {id: $to}) CREATE (s1)-[:SENT_RPC {type: $rpc, term: $term, timestamp: $ts, details: $details}]->(s2)", params)
			}
		case EventRPCReceived:
			if from, ok := e.Details["from"].(string); ok {
				params["from"] = from
				params["rpc"] = e.Details["rpc"]
				tx.Run(ctx, "MATCH (s1:Server {id: $id}) MERGE (s2:Server {id: $from}) CREATE (s2)-[:SENT_RPC {type: $rpc, term: $term, timestamp: $ts, details: $details}]->(s1)", params)
			}
		case EventTruncateLog:
			tx.Run(ctx, "MATCH (s:Server {id: $id})-[r:HAS_LOG_ENTRY]->(e:LogEntry) WHERE e.index >= $idx DELETE r", params)
		}
		return nil, nil
	})
	return err
}

func (t *Tracer) Close() {
	if t == nil {
		return
	}
	close(t.stopChan)
	if t.driver != nil {
		t.driver.Close(context.Background())
	}
}
