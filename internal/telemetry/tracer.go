package telemetry

import (
	"context"
	"encoding/json"
	"log"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

type EventType string

const (
	EventStateTransition      EventType = "StateTransition"
	EventTermChange           EventType = "TermChange"
	EventElectionStart        EventType = "ElectionStart"
	EventVoteReceived         EventType = "VoteReceived"
	EventVoteGranted          EventType = "VoteGranted"
	EventBecomeLeader         EventType = "BecomeLeader"
	EventAppendLog            EventType = "AppendLog"
	EventTruncateLog          EventType = "TruncateLog"
	EventCommit               EventType = "Commit"
	EventRPCSent              EventType = "RPCSent"
	EventRPCReceived          EventType = "RPCReceived"
	EventSnapshot             EventType = "Snapshot"
	EventHeartbeat            EventType = "Heartbeat"
	EventElectionTimeout      EventType = "ElectionTimeout"
	EventAppendEntriesResponse EventType = "AppendEntriesResponse"
	EventVoteRequest          EventType = "VoteRequest"
	EventVoteResponse         EventType = "VoteResponse"
	EventStepDown             EventType = "StepDown"
	EventLeaderChange         EventType = "LeaderChange"
)

type Event struct {
	ID            string                 `json:"id"`
	Timestamp     time.Time              `json:"timestamp"`
	TimestampUnix int64                  `json:"timestamp_unix"`
	NodeID        string                 `json:"node_id"`
	Type          EventType              `json:"type"`
	Term          uint64                 `json:"term"`
	Index         uint64                 `json:"index,omitempty"`
	TraceID       string                 `json:"trace_id,omitempty"`
	Details       map[string]interface{} `json:"details,omitempty"`
}

type Tracer struct {
	mu     sync.Mutex
	nodeID string

	// Neo4j fields
	driver   neo4j.DriverWithContext
	eventCh  chan Event
	stopChan chan struct{}

	// Batching config
	batchSize     int
	flushInterval time.Duration
}

func NewTracer(nodeID string, neo4jURI, neo4jUser, neo4jPassword string) (*Tracer, error) {
	t := &Tracer{
		nodeID:        nodeID,
		eventCh:       make(chan Event, 10000),
		stopChan:      make(chan struct{}),
		batchSize:     100,
		flushInterval: 100 * time.Millisecond,
	}

	if neo4jURI != "" {
		driver, err := neo4j.NewDriverWithContext(neo4jURI, neo4j.BasicAuth(neo4jUser, neo4jPassword, ""))
		if err != nil {
			log.Printf("[Tracer] Error: Failed to create Neo4j driver: %v", err)
		} else {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := driver.VerifyConnectivity(ctx); err != nil {
				log.Printf("[Tracer] Error: Neo4j connectivity check failed: %v", err)
				driver.Close(ctx)
			} else {
				log.Printf("[Tracer] Successfully connected to Neo4j (Batching Enabled)")
				t.driver = driver
				go t.batchWorker()
			}
		}
	}

	return t, nil
}

func (t *Tracer) Record(eventType EventType, term uint64, index uint64, details map[string]interface{}) {
	if t == nil || t.driver == nil {
		return
	}

	// Extract or generate TraceID
	traceID := ""
	if tid, ok := details["trace_id"].(string); ok {
		traceID = tid
	} else {
		traceID = uuid.New().String()
	}

	event := Event{
		ID:            uuid.New().String(),
		Timestamp:     time.Now(),
		TimestampUnix: time.Now().UnixMilli(),
		NodeID:        t.nodeID,
		Type:          eventType,
		Term:          term,
		Index:         index,
		TraceID:       traceID,
		Details:       details,
	}

	select {
	case t.eventCh <- event:
	default:
	}
}

func (t *Tracer) batchWorker() {
	ticker := time.NewTicker(t.flushInterval)
	defer ticker.Stop()

	var batch []Event
	ctx := context.Background()

	for {
		select {
		case <-t.stopChan:
			if len(batch) > 0 {
				t.flushBatch(ctx, batch)
			}
			return
		case event := <-t.eventCh:
			batch = append(batch, event)
			if len(batch) >= t.batchSize {
				t.flushBatch(ctx, batch)
				batch = nil
			}
		case <-ticker.C:
			if len(batch) > 0 {
				t.flushBatch(ctx, batch)
				batch = nil
			}
		}
	}
}

func (t *Tracer) flushBatch(ctx context.Context, batch []Event) {
	session := t.driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeWrite})
	defer session.Close(ctx)

	_, err := session.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) (interface{}, error) {
		for _, e := range batch {
			detailsJSON, _ := json.Marshal(e.Details)
			params := map[string]interface{}{
				"id":      e.NodeID,
				"eid":     e.ID,
				"term":    e.Term,
				"idx":     e.Index,
				"ts":      e.Timestamp.Format(time.RFC3339Nano),
				"ts_unix": e.TimestampUnix,
				"type":    string(e.Type),
				"tid":     e.TraceID,
				"details": string(detailsJSON),
			}

			// 1. Create the Event Node (Normalized)
			tx.Run(ctx, `
				MERGE (s:Server {id: $id})
				MERGE (t:Term {value: $term})
				CREATE (e:Event {
					id: $eid,
					type: $type,
					timestamp: $ts,
					timestamp_unix: $ts_unix,
					term: $term,
					index: $idx,
					trace_id: $tid,
					details: $details
				})
				CREATE (s)-[:EMITTED]->(e)
				CREATE (e)-[:IN_TERM]->(t)
			`, params)

			// 2. Optimized Current State Edges
			switch e.Type {
			case EventStateTransition:
				if role, ok := e.Details["to"].(string); ok {
					params["role"] = role
					tx.Run(ctx, `
						MATCH (s:Server {id: $id})
						OPTIONAL MATCH (s)-[old:CURRENT_ROLE]->()
						DELETE old
						CREATE (s)-[:CURRENT_ROLE {role: $role, since: $ts}]->(:Role {name: $role})
					`, params)
				}
			case EventTermChange:
				tx.Run(ctx, `
					MATCH (s:Server {id: $id}), (t:Term {value: $term})
					OPTIONAL MATCH (s)-[old:CURRENT_TERM]->()
					DELETE old
					CREATE (s)-[:CURRENT_TERM {since: $ts}]->(t)
				`, params)
			case EventBecomeLeader:
				tx.Run(ctx, `
					MATCH (s:Server {id: $id}), (t:Term {value: $term})
					MERGE (s)-[r:LEADER_OF]->(t)
					SET r.timestamp = $ts
				`, params)
			case EventRPCSent:
				if to, ok := e.Details["to"].(string); ok {
					params["to"] = to
					params["rpc"] = e.Details["rpc"]
					tx.Run(ctx, "MATCH (s1:Server {id: $id}) MERGE (s2:Server {id: $to}) CREATE (s1)-[:SENT_RPC {type: $rpc, term: $term, timestamp: $ts, trace_id: $tid}]->(s2)", params)
				}
			}
		}
		return nil, nil
	})

	if err != nil {
		log.Printf("[Tracer] Batch write error: %v", err)
	}
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
