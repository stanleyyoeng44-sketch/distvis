// Teaching model implementations. Application inputs are declared beside their handlers.

export function createActor(run, id) {
  const index = run.nodes.indexOf(id);
  const actor = {
    generation: 0,
    state: { role: 'follower', term: 0, votedFor: null },
    later(delay, fn) {
      const generation = this.generation;
      run.schedule(delay, () => { if (run.online[id] && this.generation === generation) fn(); });
    },
    report() { run.report(id, this.state); },
    broadcast(payload) { for (const peer of run.nodes) if (peer !== id) run.send(id, peer, payload); },
  };
  if (run.config.protocol === 'raft') {
    Object.assign(actor, {
      inputs: [{ action: 'propose', label: '提交提案', description: '任意节点可提交数字，等待 Leader 后转发。Leader 接收不代表多数派提交；此示例不含日志复制。',
        fields: [{ name: 'number', label: 'Proposed number', type: 'number', required: true, integer: true }] }],
      pending: [], seen: new Set(),
      command(cmd) {
        const proposal = { id: cmd.id, origin: id, number: cmd.values.number };
        this.pending.push(proposal);
        this.state.lastProposal = { ...proposal, status: '等待 Leader 接收' };
        this.report(); this.flush();
      },
      accept(proposal) {
        if (!this.seen.has(proposal.id)) {
          this.seen.add(proposal.id);
          this.state.lastReceivedProposal = { ...proposal, status: 'Leader 已接收（未提交）' };
          this.report();
        }
        if (proposal.origin === id) this.ack(proposal);
        else run.send(id, proposal.origin, { type: 'ProposalAck', term: this.state.term, proposal });
      },
      ack(proposal) {
        this.pending = this.pending.filter(p => p.id !== proposal.id);
        if (this.state.lastProposal?.id === proposal.id) {
          this.state.lastProposal.status = 'Leader 已接收（未提交）'; this.report();
        }
      },
      flush() {
        for (const proposal of [...this.pending]) {
          if (this.state.role === 'leader') this.accept(proposal);
          else if (this.state.leader) run.send(id, this.state.leader, { type: 'Proposal', term: this.state.term, proposal });
        }
      },
      deadline: 0, votes: new Set(),
      reset() { this.deadline = run.time + 1000 + Math.floor(run.random() * 1000); },
      start() {
        this.state.role = 'follower'; this.state.leader = null;
        this.reset(); this.report(); this.tick();
      },
      tick() {
        if (this.state.role === 'leader') this.broadcast({ type: 'Heartbeat', term: this.state.term, leader: id });
        else if (run.time >= this.deadline) {
          Object.assign(this.state, { role: 'candidate', term: this.state.term + 1, votedFor: id, leader: null });
          this.votes = new Set([id]); this.reset(); this.report();
          this.broadcast({ type: 'RequestVote', term: this.state.term, candidate: id });
        }
        this.flush(); this.later(200, () => this.tick());
      },
      receive(from, msg) {
        if (msg.term > this.state.term) {
          Object.assign(this.state, { role: 'follower', term: msg.term, votedFor: null, leader: null });
          this.reset(); this.report();
        }
        if (msg.type === 'RequestVote') {
          const granted = msg.term === this.state.term && (!this.state.votedFor || this.state.votedFor === from);
          if (granted) { this.state.votedFor = from; this.reset(); this.report(); }
          run.send(id, from, { type: 'Vote', term: this.state.term, granted });
        }
        if (msg.type === 'Vote' && msg.term === this.state.term && msg.granted && this.state.role === 'candidate') {
          this.votes.add(from);
          if (this.votes.size > run.nodes.length / 2) {
            this.state.role = 'leader'; this.state.leader = id; this.report(); this.flush();
            this.broadcast({ type: 'Heartbeat', term: this.state.term, leader: id });
          }
        }
        if (msg.type === 'Heartbeat') {
          if (msg.term === this.state.term) {
            this.state.role = 'follower'; this.state.leader = from; this.reset(); this.report();
          }
          run.send(id, from, { type: 'HeartbeatAck', term: this.state.term });
        }
        if (msg.type === 'Proposal' && this.state.role === 'leader' && msg.proposal) this.accept(msg.proposal);
        if (msg.type === 'ProposalAck' && msg.term === this.state.term && msg.proposal) this.ack(msg.proposal);
      },
    });
  } else if (run.config.protocol === 'token') {
    Object.assign(actor, {
      inputs: [{ action: 'enqueue', label: '提交 payload', description: '等待获得令牌后处理。再次提交会替换尚未处理的 payload。',
        fields: [{ name: 'payload', label: 'Payload', type: 'text', required: true, maxLength: 4096 }] }],
      state: { role: 'waiting', entries: 0, hasToken: false },
      wirePayload: null,
      start(recovered) {
        this.state.role = 'waiting'; this.state.hasToken = false; this.report();
        if (index === 0 && !recovered) this.later(500, () => this.enter());
      },
      command(cmd) {
        this.state.pendingInput = { id: cmd.id, payload: cmd.values.payload };
        if (this.state.hasToken) this.process();
        this.report();
      },
      process() {
        if (!this.state.pendingInput) return;
        this.state.lastProcessedInput = this.state.pendingInput;
        this.wirePayload = { ...this.state.pendingInput, origin: id };
        delete this.state.pendingInput;
      },
      enter() {
        this.state.role = 'critical'; this.state.hasToken = true; this.state.entries++; this.process(); this.report();
        this.later(900, () => {
          this.state.role = 'waiting'; this.state.hasToken = false; this.report();
          run.send(id, run.nodes[(index + 1) % run.nodes.length], { type: 'Token', round: this.state.entries, payload: this.wirePayload });
          this.wirePayload = null;
        });
      },
      receive(from, msg) { if (msg.type === 'Token' && !this.state.hasToken) { this.state.lastReceivedPayload = msg.payload; this.enter(); } },
    });
  } else {
    Object.assign(actor, {
      inputs: [{ action: 'write', label: '写入副本', fields: [
        { name: 'key', label: '键', type: 'text', required: true, maxLength: 100 },
        { name: 'value', label: '值', type: 'text', required: true, maxLength: 4096 }] }],
      command(cmd) { this.put(cmd.values.key, cmd.values.value); },
      state: { role: 'replica', version: 0, store: {} },
      start(recovered) {
        this.report();
        if (index === 0 && !recovered) this.put('course', 'distributed-systems');
        this.sync();
      },
      put(key, value) {
        this.state.version++;
        this.state.store[key] = { value, version: this.state.version, writer: id };
        this.report(); this.broadcast({ type: 'Sync', store: this.state.store });
      },
      sync() {
        this.broadcast({ type: 'Sync', store: this.state.store });
        this.later(1600, () => this.sync());
      },
      receive(from, msg) {
        if (msg.type !== 'Sync') return;
        let changed = false;
        for (const [key, item] of Object.entries(msg.store)) {
          this.state.version = Math.max(this.state.version, item.version);
          const old = this.state.store[key];
          if (!old || item.version > old.version || (item.version === old.version && item.writer > old.writer)) {
            this.state.store[key] = item; changed = true;
          }
        }
        if (changed) this.report();
      },
    });
  }
  return actor;
}
