const App = {
    isLive: true,
    currentTime: null,
    timeRange: { start: 0, end: 0 },
    selectedNode: null,

    async init() {
        Graph.init('cy');
        
        document.getElementById('reset-layout').onclick = () => {
            Graph.instance.layout({ name: 'circle', animate: true }).run();
        };

        document.getElementById('close-inspector').onclick = () => {
            document.getElementById('inspector').classList.add('hidden');
            this.selectedNode = null;
        };

        const slider = document.getElementById('time-slider');
        const playPauseBtn = document.getElementById('play-pause');
        const timeDisplay = document.getElementById('time-display');

        slider.oninput = (e) => {
            this.isLive = false;
            playPauseBtn.innerText = '▶';
            const pct = e.target.value / 100;
            this.currentTime = this.timeRange.start + (this.timeRange.end - this.timeRange.start) * pct;
            timeDisplay.innerText = new Date(this.currentTime).toLocaleString();
            this.sync();
        };

        playPauseBtn.onclick = () => {
            this.isLive = !this.isLive;
            playPauseBtn.innerText = this.isLive ? '⏸' : '▶';
            if (this.isLive) {
                slider.value = 100;
                timeDisplay.innerText = 'LIVE';
                this.sync();
            }
        };

        this.sync();
        setInterval(() => {
            if (this.isLive) this.sync();
        }, 2000);
    },

    async sync() {
        try {
            const now = Date.now();
            this.timeRange = await API.getTimeRange();
            const targetTime = this.isLive ? null : this.currentTime;
            const nodes = await API.getClusterState(targetTime);
            Graph.update(nodes);

            if (nodes.length > 0) {
                const maxTerm = Math.max(...nodes.map(n => n.term));
                document.getElementById('term-val').innerText = maxTerm;
            }

            const events = await API.getRecentEvents();
            this.updateEventFeed('event-feed', events);

            if (this.isLive) {
                const rpcs = await API.getRecentRPCs(now - 2000);
                rpcs.forEach(r => {
                    if (r.from && r.to) Graph.animateRpc(r.from, r.to, r.rpc);
                });
            }

            if (this.selectedNode) {
                this.updateInspector(nodes);
            }

        } catch (err) {
            console.error("Sync Error:", err);
            document.getElementById('health-val').innerText = "ERROR";
            document.getElementById('health-val').title = err.message;
            document.getElementById('health-val').style.color = "#f85149";
        }
    },

    async inspectNode(nodeId) {
        this.selectedNode = nodeId;
        document.getElementById('inspector').classList.remove('hidden');
        document.getElementById('inspect-node-id').innerText = `Node: ${nodeId}`;
        this.sync();
    },

    async updateInspector(allNodes) {
        const node = allNodes.find(n => n.id === this.selectedNode);
        if (node) {
            document.getElementById('inspect-role').innerText = node.role;
            document.getElementById('inspect-term').innerText = node.term;
        }
        
        const history = await API.getNodeHistory(this.selectedNode);
        this.updateEventFeed('inspect-events', history);

        const logs = await API.getNodeLogs(this.selectedNode);
        this.updateLogStrip(logs);
    },

    updateEventFeed(elementId, events) {
        const feed = document.getElementById(elementId);
        feed.innerHTML = events.map(e => `
            <div class="event-item">
                <div class="event-type">${e.type}</div>
                <div class="event-meta">Term: ${e.term} | ${new Date(e.ts).toLocaleTimeString()}</div>
            </div>
        `).join('');
    },

    updateLogStrip(logs) {
        const strip = document.getElementById('inspect-log-strip');
        strip.innerHTML = logs.map(l => `
            <div class="log-entry ${l.committed ? 'committed' : ''}">
                <div class="index">${l.index}</div>
                <div class="term">T${l.term}</div>
            </div>
        `).join('');
    }
};

window.onload = () => App.init();
