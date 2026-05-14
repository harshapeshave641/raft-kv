const driver = neo4j.driver("bolt://localhost:7687", neo4j.auth.basic("neo4j", "password"));

const API = {
    async getClusterState(timestamp = null) {
        const session = driver.session();
        try {
            if (timestamp) {
                const result = await session.run(`
                    MATCH (s:Server)
                    OPTIONAL MATCH (s)-[:EMITTED]->(e:Event)
                    WHERE e.timestamp_unix <= $ts
                    WITH s, e ORDER BY e.timestamp_unix DESC
                    WITH s, collect(e)[0] as latestEvent
                    RETURN s.id as id, 
                           latestEvent.details as details, 
                           latestEvent.type as type,
                           latestEvent.term as term
                `, { ts: neo4j.int(timestamp) });
                
                return result.records.map(r => {
                    const detailsStr = r.get('details');
                    let role = 'Follower';
                    let details = {};
                    try { details = JSON.parse(detailsStr || '{}'); } catch(e){}
                    if (r.get('type') === 'StateTransition') role = details.to;
                    else if (r.get('type') === 'BecomeLeader') role = 'Leader';
                    
                    return {
                        id: r.get('id'),
                        role: role,
                        term: r.get('term')?.toNumber() || 0
                    };
                });
            }

            const result = await session.run(`
                MATCH (s:Server)
                OPTIONAL MATCH (s)-[r:CURRENT_ROLE]->()
                OPTIONAL MATCH (s)-[:CURRENT_TERM]->(t:Term)
                
                // Fallback for role
                OPTIONAL MATCH (s)-[:EMITTED]->(e:Event)
                WHERE e.type = "StateTransition"
                WITH s, r, t, e ORDER BY e.timestamp_unix DESC
                WITH s, r, t, collect(e)[0] as latestEv
                
                RETURN s.id as id, 
                       coalesce(r.role, latestEv.details) as role, 
                       coalesce(t.value, latestEv.term, 0) as term
            `);
            
            return result.records.map(r => {
                let role = r.get('role');
                // Handle JSON details if role came from event
                if (role && role.includes('{')) {
                    try { role = JSON.parse(role).to; } catch(e) { role = 'Follower'; }
                }

                return {
                    id: r.get('id'),
                    role: role || 'Follower',
                    term: r.get('term')?.toNumber() || 0
                };
            });
        } finally {
            await session.close();
        }
    },

    async getTimeRange() {
        const session = driver.session();
        try {
            const result = await session.run(`
                MATCH (e:Event)
                RETURN min(e.timestamp_unix) as start, max(e.timestamp_unix) as end
            `);
            const record = result.records[0];
            return {
                start: record.get('start')?.toNumber() || Date.now(),
                end: record.get('end')?.toNumber() || Date.now()
            };
        } finally {
            await session.close();
        }
    },

    async getRecentEvents(limit = 20) {
        const session = driver.session();
        try {
            const result = await session.run(`
                MATCH (e:Event)
                ORDER BY e.timestamp_unix DESC
                LIMIT $limit
                RETURN e.id as id, e.type as type, e.timestamp as ts, e.term as term, e.details as details
            `, { limit: neo4j.int(limit) });
            
            return result.records.map(r => ({
                id: r.get('id'),
                type: r.get('type'),
                ts: r.get('ts'),
                term: r.get('term').toNumber(),
                details: r.get('details')
            }));
        } finally {
            await session.close();
        }
    },

    async getNodeHistory(nodeId, limit = 10) {
        const session = driver.session();
        try {
            const result = await session.run(`
                MATCH (s:Server {id: $id})-[:EMITTED]->(e:Event)
                ORDER BY e.timestamp_unix DESC
                LIMIT $limit
                RETURN e.id as id, e.type as type, e.timestamp as ts, e.term as term, e.details as details
            `, { id: nodeId, limit: neo4j.int(limit) });
            
            return result.records.map(r => ({
                id: r.get('id'),
                type: r.get('type'),
                ts: r.get('ts'),
                term: r.get('term').toNumber(),
                details: r.get('details')
            }));
        } finally {
            await session.close();
        }
    },

    async getRecentRPCs(sinceUnix) {
        const session = driver.session();
        try {
            // Find SENT_RPC events or Event nodes of type RPCSent
            const result = await session.run(`
                MATCH (s1:Server)-[:EMITTED]->(e:Event {type: "RPCSent"})
                WHERE e.timestamp_unix > $since
                RETURN s1.id as from, e.details as details
            `, { since: neo4j.int(sinceUnix) });
            
            return result.records.map(r => {
                let details = {};
                try { details = JSON.parse(r.get('details') || '{}'); } catch(e){}
                return {
                    from: r.get('from'),
                    to: details.to,
                    rpc: details.rpc
                };
            });
        } finally {
            await session.close();
        }
    },

    async getNodeLogs(nodeId) {
        const session = driver.session();
        try {
            // Find LogEntry nodes associated with the server (via APPLIED or implicitly via term/index in events)
            // For now, let's look at Event nodes of type AppendLog and Commit
            const result = await session.run(`
                MATCH (s:Server {id: $id})-[:EMITTED]->(e:Event)
                WHERE e.type IN ["AppendLog", "Commit"]
                WITH e.index as idx, collect(e) as events
                RETURN idx, 
                       [ev IN events WHERE ev.type = "Commit"][0] IS NOT NULL as committed,
                       [ev IN events WHERE ev.type = "AppendLog"][0].term as term
                ORDER BY idx ASC
                LIMIT 100
            `, { id: nodeId });
            
            return result.records.map(r => ({
                index: r.get('idx').toNumber(),
                term: r.get('term')?.toNumber() || 0,
                committed: r.get('committed')
            }));
        } finally {
            await session.close();
        }
    }
};
