// HotStuff-2 Interactive Demo - Frontend Application

class ConsensusDemo {
    constructor() {
        this.canvas = document.getElementById('canvas');
        this.ctx = this.canvas.getContext('2d');
        this.state = null;
        this.pollInterval = null;
        this.animationFrame = null;
        this.messages = []; // Animated messages
        
        this.initializeControls();
        this.startPolling();
        this.render();
    }

    initializeControls() {
        // Start button
        document.getElementById('startBtn').addEventListener('click', () => this.start());
        
        // Stop button
        document.getElementById('stopBtn').addEventListener('click', () => this.stop());
        
        // Reset button
        document.getElementById('resetBtn').addEventListener('click', () => this.reset());
        
        // Level select
        document.getElementById('levelSelect').addEventListener('change', (e) => {
            this.setLevel(e.target.value);
        });
        
        // Fault tolerance select
        document.getElementById('faultToleranceSelect').addEventListener('change', (e) => {
            this.resetWithParams();
        });
        
        // Fault injection buttons
        document.getElementById('crashBtn').addEventListener('click', () => this.crashRandomNode());
        document.getElementById('partitionBtn').addEventListener('click', () => this.createPartition());
        document.getElementById('healBtn').addEventListener('click', () => this.healAll());

        // Event log controls
        
        // Event log controls
        document.getElementById('clearEventsBtn').addEventListener('click', () => this.clearEvents());
        document.getElementById('exportEventsBtn').addEventListener('click', () => this.exportEvents());
    }

    async apiCall(endpoint, method = 'GET', body = null) {
        const options = { method };
        if (body) {
            options.headers = { 'Content-Type': 'application/json' };
            options.body = JSON.stringify(body);
        }
        
        try {
            const response = await fetch(`/api/${endpoint}`, options);
            if (!response.ok) {
                throw new Error(`API error: ${response.status}`);
            }
            return await response.json();
        } catch (error) {
            console.error(`API call failed: ${endpoint}`, error);
            return null;
        }
    }

    async start() {
        const result = await this.apiCall('start', 'POST');
        if (result) {
            document.getElementById('startBtn').disabled = true;
            document.getElementById('stopBtn').disabled = false;
        }
    }

    async stop() {
        const result = await this.apiCall('stop', 'POST');
        if (result) {
            document.getElementById('startBtn').disabled = false;
            document.getElementById('stopBtn').disabled = true;
        }
    }

    async reset() {
        await this.resetWithParams();
    }
    
    async resetWithParams() {
        const level = document.getElementById('levelSelect').value;
        const f = document.getElementById('faultToleranceSelect').value;
        await this.apiCall(`reset?level=${level}&f=${f}`, 'POST');
        document.getElementById('startBtn').disabled = false;
        document.getElementById('stopBtn').disabled = true;
    }

    async setLevel(level) {
        await this.resetWithParams();
    }

    async crashRandomNode() {
        if (!this.state || !this.state.nodes) return;
        
        const activeNodes = this.state.nodes.filter(n => n.status === 'active');
        if (activeNodes.length === 0) return;
        
        const node = activeNodes[Math.floor(Math.random() * activeNodes.length)];
        await this.apiCall(`crash?node=${node.id}`, 'POST');
    }

    async createPartition() {
        if (!this.state || !this.state.nodes) return;
        
        const nodeCount = this.state.nodes.length;
        const half = Math.floor(nodeCount / 2);
        
        const partition1 = [];
        const partition2 = [];
        
        for (let i = 0; i < nodeCount; i++) {
            if (i < half) {
                partition1.push(i);
            } else {
                partition2.push(i);
            }
        }
        
        await this.apiCall('partition', 'POST', { partitions: [partition1, partition2] });
    }

    async healAll() {
        if (!this.state || !this.state.nodes) return;
        
        // Use the dedicated heal endpoint that recovers all nodes and clears partitions
        await this.apiCall('heal', 'POST');
    }

    clearEvents() {
        if (!this.state) return;
        this.state.events = [];
        this.updateEventsList();
    }

    async exportEvents() {
        if (!this.state || !this.state.events) return;
        
        let csv = 'Time (s),Type,Description\n';
        for (const event of this.state.events) {
            const timeSeconds = (event.time / 1000).toFixed(3);
            const type = event.type || 'unknown';
            const description = (event.description || '').replace(/"/g, '""');
            csv += `${timeSeconds},${type},"${description}"\n`;
        }
        
        try {
            await navigator.clipboard.writeText(csv);
            const btn = document.getElementById('exportEventsBtn');
            const originalText = btn.textContent;
            btn.textContent = 'Copied!';
            btn.style.backgroundColor = 'var(--success)';
            setTimeout(() => {
                btn.textContent = originalText;
                btn.style.backgroundColor = '';
            }, 2000);
        } catch (error) {
            console.error('Failed to copy to clipboard:', error);
            alert('Failed to copy to clipboard.');
        }
    }

    startPolling() {
        this.pollInterval = setInterval(() => this.pollState(), 100);
    }

    async pollState() {
        const state = await this.apiCall('state');
        if (state) {
            this.state = state;
            this.updateUI();
        }
    }

    updateUI() {
        if (!this.state) return;

        // Update stats (convert ms to seconds with 1 decimal place)
        const simTimeSeconds = (this.state.simTime / 1000).toFixed(1);
        document.getElementById('simTime').textContent = `${simTimeSeconds}s`;
        document.getElementById('faultTolerance').textContent = this.state.faultTolerance || 1;
        document.getElementById('nodesQuorum').textContent = `${this.state.nodeCount || 4} / ${this.state.quorum || 3}`;
        document.getElementById('messagesSent').textContent = this.state.messagesSent;
        document.getElementById('messagesDropped').textContent = this.state.messagesDropped;
        document.getElementById('status').textContent = this.state.running ? 'Running' : 'Stopped';
        
        // Sync fault tolerance selector with state
        if (this.state.faultTolerance) {
            document.getElementById('faultToleranceSelect').value = this.state.faultTolerance;
        }
        
        // Update buttons
        document.getElementById('startBtn').disabled = this.state.running;
        document.getElementById('stopBtn').disabled = !this.state.running;

        // Update nodes list
        this.updateNodesList();

        // Update events list
        this.updateEventsList();
    }

    updateNodesList() {
        const nodesList = document.getElementById('nodesList');
        nodesList.innerHTML = '';

        for (const node of this.state.nodes) {
            const div = document.createElement('div');
            div.className = `node-item ${node.status}`;
            if (node.isLeader) div.classList.add('leader');

            div.innerHTML = `
                <div>
                    <span class="node-id">Node ${node.id}</span>
                    ${node.isLeader ? '<span style="color: var(--node-leader);"> (Leader)</span>' : ''}
                </div>
                <div class="node-stats">
                    <span>View: ${node.view}</span>
                    <span>Commits: ${node.commitHeight}</span>
                </div>
                <span class="node-status ${node.status}">${node.status}</span>
            `;

            nodesList.appendChild(div);
        }
    }

    updateEventsList() {
        const eventsList = document.getElementById('eventsList');
        
        // Only show last 20 events, reversed (newest first)
        const events = (this.state.events || []).slice(-20).reverse();
        
        eventsList.innerHTML = '';
        for (const event of events) {
            const div = document.createElement('div');
            div.className = `event-item ${event.type}`;
            // Convert ms to seconds with 1 decimal place
            const eventTimeSeconds = (event.time / 1000).toFixed(1);
            div.innerHTML = `
                <span class="event-time">${eventTimeSeconds}s</span>
                <span>${event.description}</span>
            `;
            eventsList.appendChild(div);
        }
    }

    render() {
        this.ctx.fillStyle = '#1a1a2e';
        this.ctx.fillRect(0, 0, this.canvas.width, this.canvas.height);

        if (this.state && this.state.nodes) {
            this.drawNetwork();
            this.drawNodes();
            this.drawMessages();
            this.drawBlockchain();
        } else {
            this.drawWaitingMessage();
        }

        this.animationFrame = requestAnimationFrame(() => this.render());
    }

    drawWaitingMessage() {
        this.ctx.fillStyle = '#a0a0a0';
        this.ctx.font = '16px Segoe UI';
        this.ctx.textAlign = 'center';
        this.ctx.fillText('Waiting for simulation...', this.canvas.width / 2, this.canvas.height / 2);
    }

    // Check if two nodes can communicate (not partitioned)
    canCommunicate(nodeA, nodeB) {
        const partitions = this.state.partitions;
        
        // No partitions = all can communicate
        if (!partitions || partitions.length === 0) {
            return true;
        }
        
        // Find which partition each node is in
        let partitionA = -1;
        let partitionB = -1;
        
        for (let i = 0; i < partitions.length; i++) {
            if (partitions[i].includes(nodeA)) {
                partitionA = i;
            }
            if (partitions[i].includes(nodeB)) {
                partitionB = i;
            }
        }
        
        // If either node is not in any partition, they can communicate
        if (partitionA === -1 || partitionB === -1) {
            return true;
        }
        
        // Same partition = can communicate
        return partitionA === partitionB;
    }

    drawNetwork() {
        const centerX = this.canvas.width / 2;
        const centerY = 220; // More padding from top
        const radius = 140;
        
        const nodes = this.state.nodes;
        const hasPartitions = this.state.partitions && this.state.partitions.length > 0;
        
        // Draw connecting lines between nodes
        for (let i = 0; i < nodes.length; i++) {
            for (let j = i + 1; j < nodes.length; j++) {
                const pos1 = this.getNodePosition(i, nodes.length, centerX, centerY, radius);
                const pos2 = this.getNodePosition(j, nodes.length, centerX, centerY, radius);
                
                const canTalk = this.canCommunicate(i, j);
                
                this.ctx.beginPath();
                this.ctx.moveTo(pos1.x, pos1.y);
                this.ctx.lineTo(pos2.x, pos2.y);
                
                if (canTalk) {
                    // Normal connection - solid line
                    this.ctx.strokeStyle = 'rgba(15, 52, 96, 0.6)';
                    this.ctx.lineWidth = 1;
                    this.ctx.setLineDash([]);
                } else {
                    // Partitioned - dashed red line
                    this.ctx.strokeStyle = 'rgba(239, 68, 68, 0.4)';
                    this.ctx.lineWidth = 1;
                    this.ctx.setLineDash([5, 5]);
                }
                
                this.ctx.stroke();
                this.ctx.setLineDash([]); // Reset dash
            }
        }
        
        // Draw partition divider line if partitions exist
        if (hasPartitions && this.state.partitions.length === 2) {
            this.drawPartitionDivider(centerX, centerY, radius, nodes.length);
        }
    }
    
    drawPartitionDivider(centerX, centerY, radius, nodeCount) {
        const partitions = this.state.partitions;
        if (partitions.length !== 2) return;
        
        // Find the "gap" between partitions
        // For a simple split, draw a line through the middle
        const partition1 = partitions[0];
        const partition2 = partitions[1];
        
        // Calculate average angle of each partition
        let angle1Sum = 0;
        let angle2Sum = 0;
        
        for (const nodeId of partition1) {
            angle1Sum += (nodeId / nodeCount) * Math.PI * 2 - Math.PI / 2;
        }
        for (const nodeId of partition2) {
            angle2Sum += (nodeId / nodeCount) * Math.PI * 2 - Math.PI / 2;
        }
        
        const avgAngle1 = angle1Sum / partition1.length;
        const avgAngle2 = angle2Sum / partition2.length;
        
        // Draw a dividing line perpendicular to the line connecting partition centers
        const midAngle = (avgAngle1 + avgAngle2) / 2;
        const perpAngle = midAngle + Math.PI / 2;
        
        const lineLength = radius + 60;
        const x1 = centerX + Math.cos(perpAngle) * lineLength;
        const y1 = centerY + Math.sin(perpAngle) * lineLength;
        const x2 = centerX - Math.cos(perpAngle) * lineLength;
        const y2 = centerY - Math.sin(perpAngle) * lineLength;
        
        this.ctx.beginPath();
        this.ctx.moveTo(x1, y1);
        this.ctx.lineTo(x2, y2);
        this.ctx.strokeStyle = 'rgba(239, 68, 68, 0.6)';
        this.ctx.lineWidth = 2;
        this.ctx.setLineDash([10, 5]);
        this.ctx.stroke();
        this.ctx.setLineDash([]);
        
        // Draw "PARTITION" label
        this.ctx.fillStyle = 'rgba(239, 68, 68, 0.8)';
        this.ctx.font = 'bold 11px Segoe UI';
        this.ctx.textAlign = 'center';
        this.ctx.save();
        this.ctx.translate(centerX, centerY - radius - 30);
        this.ctx.fillText('NETWORK PARTITION', 0, 0);
        this.ctx.restore();
    }

    getNodePosition(index, total, centerX, centerY, radius) {
        const angle = (index / total) * Math.PI * 2 - Math.PI / 2;
        return {
            x: centerX + Math.cos(angle) * radius,
            y: centerY + Math.sin(angle) * radius
        };
    }

    drawNodes() {
        const centerX = this.canvas.width / 2;
        const centerY = 220; // Match the network centerY
        const radius = 140;
        const nodeRadius = 35;

        for (let i = 0; i < this.state.nodes.length; i++) {
            const node = this.state.nodes[i];
            const pos = this.getNodePosition(i, this.state.nodes.length, centerX, centerY, radius);

            // Node circle
            this.ctx.beginPath();
            this.ctx.arc(pos.x, pos.y, nodeRadius, 0, Math.PI * 2);
            
            // Fill based on status
            let fillColor;
            switch (node.status) {
                case 'crashed':
                    fillColor = '#ef4444'; // Red for crashed
                    break;
                case 'partitioned':
                    fillColor = '#fbbf24'; // Yellow for partitioned
                    break;
                default:
                    // Leader gets blue, others get green (crown already indicates leader)
                    fillColor = node.isLeader ? '#3b82f6' : '#4ade80';
            }
            
            this.ctx.fillStyle = fillColor;
            this.ctx.fill();
            
            // Border
            this.ctx.strokeStyle = '#eaeaea';
            this.ctx.lineWidth = 2;
            this.ctx.stroke();

            // Node ID
            this.ctx.fillStyle = '#ffffff';
            this.ctx.font = 'bold 14px Segoe UI';
            this.ctx.textAlign = 'center';
            this.ctx.textBaseline = 'middle';
            this.ctx.fillText(`R${node.id}`, pos.x, pos.y - 8);

            // View and commits
            this.ctx.font = '10px Segoe UI';
            this.ctx.fillText(`V:${node.view}`, pos.x, pos.y + 5);
            this.ctx.fillText(`C:${node.commitHeight}`, pos.x, pos.y + 18);

            // Leader crown
            if (node.isLeader) {
                this.drawCrown(pos.x, pos.y - nodeRadius - 15);
            }
        }
    }

    drawCrown(x, y) {
        this.ctx.fillStyle = '#fbbf24';
        this.ctx.beginPath();
        this.ctx.moveTo(x - 10, y + 8);
        this.ctx.lineTo(x - 10, y);
        this.ctx.lineTo(x - 5, y + 5);
        this.ctx.lineTo(x, y - 5);
        this.ctx.lineTo(x + 5, y + 5);
        this.ctx.lineTo(x + 10, y);
        this.ctx.lineTo(x + 10, y + 8);
        this.ctx.closePath();
        this.ctx.fill();
    }

    drawMessages() {
        // Draw animated messages (simplified - just showing recent events)
        // In a full implementation, we'd track individual message animations
    }

    drawBlockchain() {
        // Draw committed blocks at the bottom
        const y = this.canvas.height - 60;
        const blockWidth = 50;
        const blockHeight = 40;
        const startX = 50;
        const maxBlocks = 12;

        // Get the maximum commit height
        let maxHeight = 0;
        for (const node of this.state.nodes) {
            if (node.commitHeight > maxHeight) {
                maxHeight = node.commitHeight;
            }
        }

        // Draw blocks
        const blocksToShow = Math.min(maxHeight, maxBlocks);
        const startBlock = Math.max(0, maxHeight - maxBlocks);

        this.ctx.fillStyle = '#a0a0a0';
        this.ctx.font = '12px Segoe UI';
        this.ctx.textAlign = 'left';
        this.ctx.fillText('Committed Blocks:', startX, y - 20);

        for (let i = 0; i < blocksToShow; i++) {
            const blockNum = startBlock + i + 1;
            const x = startX + i * (blockWidth + 10);

            // Block rectangle
            this.ctx.fillStyle = '#4ade80';
            this.ctx.fillRect(x, y, blockWidth, blockHeight);

            // Block number
            this.ctx.fillStyle = '#1a1a2e';
            this.ctx.font = 'bold 12px Segoe UI';
            this.ctx.textAlign = 'center';
            this.ctx.fillText(`#${blockNum}`, x + blockWidth / 2, y + blockHeight / 2 + 4);

            // Chain link
            if (i > 0) {
                this.ctx.strokeStyle = '#a0a0a0';
                this.ctx.lineWidth = 2;
                this.ctx.beginPath();
                this.ctx.moveTo(x - 10, y + blockHeight / 2);
                this.ctx.lineTo(x, y + blockHeight / 2);
                this.ctx.stroke();
            }
        }

        if (maxHeight === 0) {
            this.ctx.fillStyle = '#a0a0a0';
            this.ctx.font = '12px Segoe UI';
            this.ctx.textAlign = 'left';
            this.ctx.fillText('No blocks committed yet...', startX, y + 25);
        }
    }
}

// Initialize the demo when the page loads
window.addEventListener('DOMContentLoaded', () => {
    window.simulator = new ConsensusDemo();
});
