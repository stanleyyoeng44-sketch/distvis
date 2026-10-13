import { EventEmitter } from 'node:events';
import { randomUUID } from 'node:crypto';
import { createActor } from './protocols.js';
import { networkPolicy, networkProfiles } from './network.js';
import { validateSchema, validateValues } from './inputs.js';

export const protocols = {
  raft: { name: 'Raft leader election', description: 'Watch terms, votes and heartbeats, plus re-election after faults. This example has no log replication.' },
  token: { name: 'Token ring mutual exclusion', description: 'Only the node holding the token enters the critical section; a crash can lose the token and the protocol must recover on its own.' },
  gossip: { name: 'LWW replicated store', description: 'Periodic anti-entropy spreads key-value pairs, breaking ties by (logical version, node ID); converges after partitions heal.' },
  custom: { name: 'Go project in the editor', description: 'Runs the multi-file project saved in the protocol workspace; lab.Main manages nodes and standard RPC calls are recorded automatically.' },
};

export function validateConfig(input) {
  const config = {
    name: String(input.name || 'Untitled experiment').slice(0, 100),
    protocol: input.protocol || 'raft',
    runtime: input.runtime || 'docker',
    nodeCount: Number(input.nodeCount ?? 5),
    seed: Number(input.seed ?? 42),
    latency: Number(input.latency ?? 80),
    bandwidth: Number(input.bandwidth ?? 128),
    delayModel: input.delayModel || 'fixed',
    jitter: Number(input.jitter ?? 0),
    ...(input.experimentId ? {experimentId:input.experimentId} : {}),
    ...(input.protocolId ? {protocolId:input.protocolId,protocolRevision:input.protocolRevision} : {}),
    parameters: structuredClone(input.parameters || {}),
    limits: {eventCount:Number(input.limits?.eventCount || 60000),durationMs:Number(input.limits?.durationMs || 600000),messageBytes:Number(input.limits?.messageBytes ?? 65536),stdoutBytes:Number(input.limits?.stdoutBytes ?? 4194304)},
    network: networkPolicy(input.network),
    networkProfiles: networkProfiles(input.networkProfiles),
    ...(input.project ? { project: structuredClone(input.project) } : {}),
  };
  if (!protocols[config.protocol] && !/^directory:[^/\\]+$/.test(config.protocol)) throw new Error('Unknown protocol');
  if (!['simulation', 'docker', 'kubernetes'].includes(config.runtime)) throw new Error('Unknown runtime');
  for (const [key, min, max] of [['nodeCount', 2, 64], ['seed', 1, 2147483647], ['latency', 0, 30000], ['bandwidth', 1, 100000], ['jitter', 0, 30000]]) {
    if (!Number.isInteger(config[key]) || config[key] < min || config[key] > max) throw new Error(`${key} must be an integer between ${min} and ${max}`);
  }
  for(const [key,max] of [['eventCount',2000000],['durationMs',3600000],['messageBytes',4194304],['stdoutBytes',2147483648]])if(!Number.isSafeInteger(config.limits[key])||config.limits[key]<1||config.limits[key]>max)throw new Error('limits out of range');
  if(Buffer.byteLength(JSON.stringify(config.parameters))>8192)throw new Error('parameters exceed 8 KiB');
  validateDelayModel(config);
  if (config.delayModel === 'fixed') config.jitter = 0;
  if (config.runtime === 'simulation' && (config.protocol === 'custom' || config.protocol.startsWith('directory:'))) throw new Error('Custom Go programs must use Docker or Kubernetes');
  return config;
}

export const delayModels = ['fixed', 'exponential'];
function validateDelayModel(rule) {
  if (!delayModels.includes(rule.delayModel)) throw new Error('delayModel must be fixed or exponential');
  if (rule.delayModel === 'exponential' && rule.jitter < 1) throw new Error('The exponential jitter mean must be 1–30000 ms');
}

// All protocol traffic traverses this transport. No protocol-specific fault rules.
export class Experiment extends EventEmitter {
  constructor(config, { id = randomUUID(), createdAt = new Date().toISOString(), record = () => {} } = {}) {
    super();
    this.id = id;
    this.createdAt = createdAt;
    this.config = validateConfig(config);
    this.record = record;
    this.events = [];
    this.time = 0;
    this.status = 'created';
    this.nodes = Array.from({ length: this.config.nodeCount }, (_, i) => `node-${i + 1}`);
    this.online = Object.fromEntries(this.nodes.map(n => [n, true]));
    this.epochs = Object.fromEntries(this.nodes.map(n => [n, 0]));
    this.states = {};
    this.links = {};
    this.queue = [];
    this.serial = 0;
    this.messageSeq = 0;
    this.randomState = this.config.seed;
    this.nextWire = {};
    this.lastArrival = {};
    this.actors = {};
    this.inputSchemas = {};
    this.durationLimit = this.config.limits.durationMs;
    this.eventLimit = this.config.limits.eventCount;
    this.channels = {};
    this.network=structuredClone(this.config.network);this.networkProfiles=structuredClone(this.config.networkProfiles);
    this.transportStats = {requests:0, wireBytes:0, perNode:{}};
  }
  random() {
    this.randomState = (Math.imul(this.randomState, 1664525) + 1013904223) >>> 0;
    return this.randomState / 4294967296;
  }
  log(type, details = {}) {
    if (this.status === 'running' && this.events.length >= this.eventLimit - 1 && type !== 'lifecycle') {
      this.stop(`Event limit ${this.eventLimit}`);
      return;
    }
    const event = { ...structuredClone(details), seq: (this.archivedEventCount || 0) + this.events.length + 1, time: this.time, timestamp: new Date(Date.parse(this.createdAt) + this.time).toISOString(), type };
    this.record(event); // Persist before publication.
    this.events.push(event);
    this.emit('event', event);
    return event;
  }
  info() {
    return { id: this.id, createdAt: this.createdAt, config: this.config, status: this.status, time: this.time, eventCount: (this.archivedEventCount || 0) + this.events.length, transportStats: this.transportStats, ...(this.testResult ? {testResult:this.testResult} : {}) };
  }
  schedule(delay, fn) {
    this.queue.push({ at: this.time + Math.max(0, delay), order: this.serial++, fn });
    this.queue.sort((a, b) => a.at - b.at || a.order - b.order);
  }
  advance(ms) {
    if (this.status !== 'running') return;
    const target = Math.min(this.time + ms, this.durationLimit);
    while (this.queue.length && this.queue[0].at <= target && this.status === 'running') {
      const next = this.queue.shift();
      this.time = next.at;
      next.fn();
      if (this.events.length >= this.eventLimit) this.stop(`Event limit ${this.eventLimit}`);
    }
    this.time = target;
    if (this.time >= this.durationLimit) this.stop('Run duration limit of 10 minutes');
  }
  report(node, state) {
    if (!this.online[node] || this.status !== 'running') return;
    this.states[node] = structuredClone(state);
    this.log('state', { node, state });
  }
  send(from, to, payload) {
    if (this.status !== 'running' || !this.online[from]) return;
    if (!this.nodes.includes(to)) throw new Error(`Unknown destination node ${to}`);
    const id = `${this.id.slice(0, 8)}-${++this.messageSeq}`;
    const key = `${from}>${to}`;
    const link = this.links[key] || {};
    const channel = payload.channel ? this.channels[payload.channel] : null;
    const control = false; // No protocol payload may bypass network policy.
    const category = payload.type === 'RPCResponse' ? 'response' : 'request';
    const policy = networkPolicy(link.network || {}, this.networkProfiles[category] || this.network);
    const wireBytes = typeof payload.wire==='string' ? Buffer.from(payload.wire,'base64').length : 0;
    if(!control && payload.type==='RPCRequest'){this.transportStats.requests++;this.transportStats.wireBytes+=wireBytes;}
    const blocked = !control && (link.blocked || (channel && !channel.enabled));
    const bytes = Buffer.byteLength(JSON.stringify(payload));
    const bandwidth = link.bandwidth ?? this.config.bandwidth; // KiB/s, per directed link
    const wireStart = Math.max(this.time, this.nextWire[key] || 0);
    const wireEnd = wireStart + bytes / (bandwidth * 1024) * 1000;
    this.nextWire[key] = wireEnd;
    const model = link.delayModel ?? this.config.delayModel, mean = link.jitter ?? this.config.jitter;
    // Exponential jitter on top of the fixed latency; seeded, so a model run stays reproducible.
    const jitter = model === 'exponential' ? Math.round(-mean * Math.log(1 - this.random())) : 0;
    // Each directed link stays FIFO like a TCP connection: jitter delays, never reorders.
    const reordered = !control && policy.reorderRate>0 && this.random()<policy.reorderRate;
    const reorderDelay = reordered ? policy.reorderMin + Math.floor(this.random()*(policy.reorderMax-policy.reorderMin+1)) : 0;
    const candidate = control ? this.time : wireEnd + (link.latency ?? this.config.latency) + jitter + reorderDelay;
    const arrival = control || policy.ordering==='unordered' ? candidate : Math.max(candidate,this.lastArrival[key] || 0);
    this.lastArrival[key] = arrival;
    const delay = arrival - this.time;
    const message = { id, from, to, payload: structuredClone(payload), bytes, wireBytes, delay, ...(reordered?{reorderDelay}:{}), ...(model === 'exponential' ? { jitter } : {}) };
    this.log('send', message);
    if (this.status !== 'running') return;
    const sourceEpoch = this.epochs[from], targetEpoch = this.epochs[to];
    const lost = !control && policy.lossRate>0 && this.random()<policy.lossRate;
    const discard = reason => {this.log('drop',{...message,reason});if(policy.failureMode==='notify')this.emit('delivery-failed',{...message,reason,sourceEpoch,targetEpoch});};
    if (blocked || !this.online[to] || lost) {
      discard(blocked ? 'Link blocked' : lost ? 'Random packet loss' : 'Destination node offline');
      return id;
    }
    this.schedule(delay, () => {
      // In-flight packets are also affected by a newly injected partition/crash.
      if (!this.online[to] || !this.online[from] || (!control && (this.links[key]?.blocked || (payload.channel && this.channels[payload.channel]?.enabled===false))) || this.epochs[from] !== sourceEpoch || this.epochs[to] !== targetEpoch) {
        discard('A fault occurred during delivery');
        return;
      }
      if(!control && payload.type==='RPCRequest')this.transportStats.perNode[to]=(this.transportStats.perNode[to]||0)+1;
      if(!control && payload.type==='RPCResponse')this.transportStats.wireBytes+=wireBytes;
      if (this.config.runtime === 'simulation') {
        this.log('receive', message);
        this.actors[to].receive(from, message.payload);
      } else {
        this.emit('deliver', message);
      }
    });
    return id;
  }
  start() {
    this.status = 'running';
    this.log('lifecycle', { action: 'start', config: this.config, nodes: this.nodes });
    if (this.config.runtime === 'simulation') {
      for (const node of this.nodes) this.actors[node] = createActor(this, node);
      for (const node of this.nodes) this.declareInputs(node, this.actors[node].inputs);
      for (const node of this.nodes) this.actors[node].start();
    }
  }
  stop(reason = 'Stopped by the user') {
    if (['completed', 'failed', 'interrupted'].includes(this.status)) return;
    this.status = 'completed';
    this.queue = [];
    this.log('lifecycle', { action: 'stop', reason });
    this.emit('stop');
  }
  validateFault(fault) {
    if (this.status !== 'running') throw new Error('The experiment is not running');
    if (!['crash', 'recover', 'link', 'heal','network','channel'].includes(fault.kind)) throw new Error('Unknown fault kind');
    if (['crash', 'recover'].includes(fault.kind) && !this.nodes.includes(fault.node)) throw new Error('Unknown node');
    if(fault.kind==='network'){if(fault.network)networkPolicy(fault.network);if(fault.profiles)networkProfiles(fault.profiles);}
    if(fault.kind==='channel' && (typeof fault.id!=='string'||fault.id.length>128||typeof fault.enabled!=='boolean'))throw new Error('Invalid channel fault arguments');
    if (fault.kind === 'link') {
      if(fault.network)networkPolicy(fault.network);
      if (!this.nodes.includes(fault.from) || !this.nodes.includes(fault.to) || fault.from === fault.to) throw new Error('Choose two different nodes');
      for (const [key, min, max] of [['latency', 0, 30000], ['bandwidth', 1, 100000]]) {
        if (!Number.isInteger(fault[key]) || fault[key] < min || fault[key] > max) throw new Error(`${key} out of range`);
      }
      if (typeof fault.blocked !== 'boolean' || typeof fault.bidirectional !== 'boolean') throw new Error('Link options must be booleans');
      if (fault.delayModel !== undefined || fault.jitter !== undefined) {
        if (!Number.isInteger(fault.jitter ?? 0) || (fault.jitter ?? 0) < 0 || fault.jitter > 30000) throw new Error('jitter out of range');
        validateDelayModel({ delayModel: fault.delayModel ?? 'fixed', jitter: fault.jitter ?? 0 });
      }
    }
  }
  fault(fault) {
    this.validateFault(fault);
    this.log('fault', { fault });
    if (fault.kind === 'crash' || fault.kind === 'recover') {
      const on = fault.kind === 'recover';
      if (this.online[fault.node] === on) return;
      this.online[fault.node] = on;
      this.epochs[fault.node]++;
      this.log('node', { node: fault.node, online: on });
      if (this.config.runtime === 'simulation') {
        // Actors retain local protocol state (Raft term/votedFor included) across crash.
        this.actors[fault.node].generation++;
        if (on) this.actors[fault.node].start(true);
      }
    } else if(fault.kind==='network'){
      if(fault.network)this.network=networkPolicy(fault.network);
      if(fault.profiles)this.networkProfiles=networkProfiles(fault.profiles);
    } else if(fault.kind==='channel'){
      this.channels[fault.id]={enabled:fault.enabled,from:fault.from,to:fault.to};
    } else if (fault.kind === 'heal') {
      this.links = {};
      for(const c of Object.values(this.channels))c.enabled=true;
    } else {
      const rule = { latency: fault.latency, bandwidth: fault.bandwidth, blocked: fault.blocked, ...(fault.network?{network:networkPolicy(fault.network)}:{}) };
      // Omitted delay model inherits the run's; an explicit fixed model disables jitter on this link.
      if (fault.delayModel !== undefined) Object.assign(rule, { delayModel: fault.delayModel, jitter: fault.delayModel === 'fixed' ? 0 : fault.jitter });
      this.links[`${fault.from}>${fault.to}`] = rule;
      if (fault.bidirectional) this.links[`${fault.to}>${fault.from}`] = rule;
    }
  }
  declareInputs(node, schema) {
    if (!this.nodes.includes(node) || this.status !== 'running') throw new Error('The node is not running');
    validateSchema(schema);
    this.inputSchemas[node] = structuredClone(schema);
    this.log('input_schema', { node, schema });
  }
  // Validation shared by single and batched inputs; nothing is logged until every input is valid.
  prepareCommand(node, key, value, action = 'write', values) {
    if (this.status !== 'running' || !this.nodes.includes(node) || !this.online[node]) throw new Error(`${node} is not running`);
    if (this.mutating) throw new Error('A fault operation is in progress; try again shortly');
    const schema = this.inputSchemas[node]?.find(s => s.action === action);
    if (!schema) throw new Error(`${node} has not declared this input action`);
    // Retain the original key/value API for existing clients.
    if (values === undefined) values = { ...(key === undefined ? {} : { key }), ...(value === undefined ? {} : { value }) };
    validateValues(schema, values, this.nodes);
    const cmd = { node, action, values: structuredClone(values) };
    if (typeof values.key === 'string') cmd.key = values.key;
    if (typeof values.value === 'string') cmd.value = values.value;
    return cmd;
  }
  dispatch(cmd, extra = {}) {
    const event = this.log('command', { ...cmd, ...extra });
    if (!event || this.status !== 'running') throw new Error('The experiment has ended');
    cmd.id = `input-${event.seq}`;
    try {
      if (this.config.runtime === 'simulation') this.actors[cmd.node].command(cmd);
      else if (!this.emit('command', cmd)) throw new Error('The node session is not writable');
    } catch (error) {
      this.log('command_error', { node: cmd.node, commandSeq: event.seq, message: error.message });
      throw error;
    }
    return { commandSeq: event.seq, id: cmd.id };
  }
  command(node, key, value, action = 'write', values) {
    return this.dispatch(this.prepareCommand(node, key, value, action, values));
  }
  // Concurrent inputs: all are validated first, then delivered at the same coordinator time,
  // so the nodes start their operations without any causal relation between them.
  commandBatch(batch) {
    if (!Array.isArray(batch) || batch.length < 1 || batch.length > this.nodes.length) throw new Error(`batch must contain 1–${this.nodes.length} inputs`);
    if (new Set(batch.map(b => b?.node)).size !== batch.length) throw new Error('Each node may appear at most once in a batch');
    const cmds = batch.map(b => this.prepareCommand(b.node, undefined, undefined, b.action, b.values ?? {}));
    const group = `batch-${this.events.length + 1}`, nodes = cmds.map(c => c.node);
    const results = [];
    for (const cmd of cmds) {
      try { results.push({ node: cmd.node, ...this.dispatch(cmd, { batch: group, concurrentWith: nodes.filter(n => n !== cmd.node) }) }); }
      catch (error) { results.push({ node: cmd.node, error: error.message }); }
    }
    return { batch: group, commands: results };
  }
}
