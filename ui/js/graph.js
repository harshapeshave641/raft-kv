const Graph = {
    instance: null,

    init(containerId) {
        this.instance = cytoscape({
            container: document.getElementById(containerId),
            style: [
                {
                    selector: 'node',
                    style: {
                        'label': 'data(id)',
                        'color': '#fff',
                        'font-family': 'Outfit',
                        'font-size': '12px',
                        'text-valign': 'bottom',
                        'text-margin-y': '8px',
                        'background-color': '#30363d',
                        'width': '40px',
                        'height': '40px',
                        'transition-property': 'background-color, border-color, border-width',
                        'transition-duration': '0.3s'
                    }
                },
                {
                    selector: 'node:selected',
                    style: {
                        'border-width': '2px',
                        'border-color': '#58a6ff'
                    }
                },
                {
                    selector: 'node[role="Leader"]',
                    style: {
                        'background-color': '#f2cc60',
                        'border-width': '4px',
                        'border-color': '#e3b341',
                        'width': '50px',
                        'height': '50px'
                    }
                },
                {
                    selector: 'node[role="Candidate"]',
                    style: {
                        'background-color': '#e07b3a',
                        'border-width': '2px',
                        'border-color': '#d16a29'
                    }
                },
                {
                    selector: 'node[role="Follower"]',
                    style: {
                        'background-color': '#3fb950'
                    }
                },
                {
                    selector: 'edge',
                    style: {
                        'width': 2,
                        'line-color': 'var(--border)',
                        'target-arrow-color': 'var(--border)',
                        'target-arrow-shape': 'triangle',
                        'curve-style': 'bezier',
                        'opacity': 0.3
                    }
                },
                {
                    selector: 'edge.rpc-active',
                    style: {
                        'width': 4,
                        'line-color': '#58a6ff',
                        'target-arrow-color': '#58a6ff',
                        'opacity': 1,
                        'z-index': 100
                    }
                }
            ],
            layout: { name: 'circle' }
        });

        this.instance.on('tap', 'node', (evt) => {
            const node = evt.target;
            App.inspectNode(node.data('id'));
        });
    },

    update(nodes) {
        if (!this.instance) return;
        const elements = nodes.map(n => ({ data: { id: n.id, role: n.role } }));
        this.instance.json({ elements });
        this.instance.layout({ name: 'circle', animate: true }).run();
    },

    animateRpc(from, to, type) {
        if (!this.instance) return;
        
        const edgeId = `rpc-${from}-${to}-${Date.now()}`;
        const edge = this.instance.add({
            group: 'edges',
            data: { id: edgeId, source: from, target: to },
            classes: 'rpc-active'
        });

        // Pulse and fade out
        edge.animate({
            style: { opacity: 0, 'width': 1 },
            duration: 1000,
            complete: () => {
                this.instance.remove(edge);
            }
        });
    }
};
