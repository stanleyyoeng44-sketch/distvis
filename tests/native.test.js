import { test } from 'node:test';
import assert from 'node:assert/strict';
import { spawn } from 'node:child_process';
import { mkdtempSync, mkdirSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { createInterface } from 'node:readline';
import { Experiment } from '../server/engine.js';

// Optional integration suite: compile examples/node and set DISTVIS_NODE_BINARY.
// This verifies real Go processes + SDK + network transport, independently of container availability.
const binary = process.env.DISTVIS_NODE_BINARY;
async function waitFor(predicate, timeout = 12000) {
  const deadline = Date.now() + timeout;
  while (Date.now() < deadline) {
    if (predicate()) return;
    await new Promise(resolve => setTimeout(resolve, 50));
  }
  assert.fail('Timed out waiting for protocol state');
}
function cluster(protocol) {
  const directory = mkdtempSync(join(tmpdir(), 'distvis-native-'));
  const run = new Experiment({ protocol, runtime: 'docker', nodeCount: 3, latency: 40, bandwidth: 128 });
  const processes = new Map(), pending = new Map(), diagnostics = [];
  function start(node) {
    const path = join(directory, node); mkdirSync(path, { recursive: true });
    const child = spawn(binary, [], { cwd: path, stdio: ['pipe', 'pipe', 'pipe'] });
    processes.set(node, child);
    child.stdin.on('error', () => {});
    child.stderr.on('data', b => diagnostics.push(b.toString()));
    createInterface({ input: child.stdout }).on('line', line => {
      if (!run.online[node]) return;
      const record = JSON.parse(line);
      if (record.type === 'send') run.send(node, record.to, record.payload);
      if (record.type === 'state') run.report(node, record.state);
      if (record.type === 'input_schema') run.declareInputs(node, record.schema);
      if (record.type === 'received' && pending.has(record.id)) {
        run.log('receive', pending.get(record.id)); pending.delete(record.id);
      }
    });
    child.stdin.write(JSON.stringify({ type: 'init', node, nodes: run.nodes, protocol }) + '\n');
  }
  run.on('deliver', msg => {
    const child = processes.get(msg.to);
    if (child?.stdin.writable) {
      pending.set(msg.id, msg);
      child.stdin.write(JSON.stringify({ type: 'message', ...msg }) + '\n');
    }
  });
  run.on('command', ({ node, ...cmd }) => {
    processes.get(node).stdin.write(JSON.stringify({ type: 'command', ...cmd }) + '\n');
  });
  run.start();
  run.nodes.forEach(start);
  const timer = setInterval(() => run.advance(20), 20);
  return {
    run, diagnostics,
    async crash(node) {
      run.fault({ kind: 'crash', node });
      const child = processes.get(node);
      await new Promise(resolve => { child.once('close', resolve); child.kill('SIGKILL'); });
    },
    recover(node) { run.fault({ kind: 'recover', node }); start(node); },
    async close() {
      clearInterval(timer); run.stop();
      await Promise.all([...processes.values()].filter(c => c.exitCode === null && c.signalCode === null).map(c => new Promise(resolve => { c.once('close', resolve); c.kill(); })));
    },
  };
}
test('native Go Raft nodes elect, crash, persist votes/term and rejoin', { skip: !binary, timeout: 30000 }, async () => {
  const c = cluster('raft'), { run } = c;
  const leaders = () => run.nodes.filter(n => run.online[n] && run.states[n]?.role === 'leader');
  try {
    await waitFor(() => leaders().length === 1);
    const inputNode = run.nodes.find(n => n !== leaders()[0]);
    run.command(inputNode, undefined, undefined, 'propose', { number: 42 });
    await waitFor(() => run.states[inputNode]?.lastProposal?.status === 'Leader 已接收（未提交）');
    assert.equal(run.states[leaders()[0]].lastReceivedProposal.number, 42);
    assert.ok(run.events.some(e => e.type === 'receive' && e.payload.type === 'Proposal' && e.payload.proposal.number === 42));
    const old = leaders()[0], term = run.states[old].term;
    await c.crash(old);
    await waitFor(() => leaders().length === 1);
    assert.ok(run.states[leaders()[0]].term > term);
    c.recover(old);
    await waitFor(() => run.states[old]?.role === 'follower' && run.states[old].term > term);
    assert.equal(leaders().length, 1);
    assert.ok(run.events.some(e => e.type === 'receive'));
    assert.deepEqual(c.diagnostics, []);
  } finally { await c.close(); }
});
test('native Go LWW nodes converge after partition and commands', { skip: !binary, timeout: 30000 }, async () => {
  const c = cluster('gossip'), { run } = c;
  try {
    await waitFor(() => run.nodes.every(n => run.states[n]?.store?.course));
    for (const to of run.nodes.slice(1)) run.fault({ kind: 'link', from: 'node-1', to, blocked: true, latency: 40, bandwidth: 128, bidirectional: true });
    run.command('node-1', 'x', 'left'); run.command('node-2', 'x', 'right');
    await waitFor(() => run.states['node-1'].store.x && run.states['node-2'].store.x);
    assert.equal(run.states['node-1'].store.x.value, 'left');
    run.fault({ kind: 'heal' });
    await waitFor(() => run.nodes.every(n => run.states[n]?.store?.x?.value === 'right'));
    assert.deepEqual(c.diagnostics, []);
  } finally { await c.close(); }
});
test('native Go token ring visits all nodes with mutual exclusion', { skip: !binary, timeout: 20000 }, async () => {
  const c = cluster('token'), { run } = c;
  try {
    await waitFor(() => run.inputSchemas['node-3']);
    run.command('node-3', undefined, undefined, 'enqueue', { payload: 'native-payload' });
    await waitFor(() => run.states['node-3']?.lastProcessedInput?.payload === 'native-payload');
    await waitFor(() => run.events.some(e => e.type === 'receive' && e.payload.type === 'Token' && e.payload.payload?.payload === 'native-payload'));
    await waitFor(() => run.nodes.every(n => run.states[n]?.entries > 0));
    const state = {};
    for (const e of run.events) if (e.type === 'state') {
      state[e.node] = e.state;
      assert.ok(Object.values(state).filter(s => s.hasToken).length <= 1);
    }
    assert.deepEqual(c.diagnostics, []);
  } finally { await c.close(); }
});
