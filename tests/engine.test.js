import { test } from 'node:test';
import assert from 'node:assert/strict';
import { Experiment, validateConfig } from '../server/engine.js';

function create(protocol = 'raft', extra = {}) {
  const run = new Experiment({ protocol, runtime: 'simulation', nodeCount: 5, seed: 42, ...extra }, { id: 'test-run', createdAt: '2026-01-01T00:00:00.000Z' });
  run.start(); return run;
}
const leaders = run => run.nodes.filter(n => run.online[n] && run.states[n]?.role === 'leader');
function link(run, from, to, blocked, extra = {}) {
  run.fault({ kind: 'link', from, to, blocked, latency: 80, bandwidth: 128, bidirectional: true, ...extra });
}
test('Raft elects a leader and reelects after its crash; recovered term catches up', () => {
  const run = create(); run.advance(6000);
  assert.equal(leaders(run).length, 1);
  const old = leaders(run)[0], term = run.states[old].term;
  run.fault({ kind: 'crash', node: old }); run.advance(6000);
  assert.equal(leaders(run).length, 1);
  assert.notEqual(leaders(run)[0], old);
  assert.ok(run.states[leaders(run)[0]].term > term);
  run.fault({ kind: 'recover', node: old }); run.advance(6000);
  assert.equal(leaders(run).length, 1);
  assert.equal(run.states[old].role, 'follower');
});
test('minority cannot elect a leader; healing restores election', () => {
  const run = create();
  for (let i = 0; i < 2; i++) for (let j = 2; j < 5; j++) link(run, run.nodes[i], run.nodes[j], true);
  run.advance(12000);
  assert.ok(!run.nodes.slice(0, 2).some(n => run.states[n].role === 'leader'));
  assert.equal(leaders(run).length, 1);
  run.fault({ kind: 'heal' }); run.advance(12000);
  assert.equal(leaders(run).length, 1);
});
test('each term has at most one elected leader across seeds and partitions', () => {
  for (let seed = 1; seed <= 20; seed++) {
    const run = create('raft', { seed }); run.advance(4000);
    for (const to of run.nodes.slice(2)) for (const from of run.nodes.slice(0, 2)) link(run, from, to, true);
    run.advance(9000); run.fault({ kind: 'heal' }); run.advance(9000);
    const terms = new Map();
    for (const e of run.events) if (e.type === 'state' && e.state.role === 'leader') {
      if (terms.has(e.state.term)) assert.equal(terms.get(e.state.term), e.node);
      terms.set(e.state.term, e.node);
    }
  }
});
test('LWW replicas converge after partition with conflicting concurrent writes', () => {
  const run = create('gossip'); run.advance(2000);
  for (const node of run.nodes.slice(1)) link(run, 'node-1', node, true);
  run.command('node-1', 'x', 'left'); run.command('node-2', 'x', 'right');
  run.advance(4000);
  assert.notEqual(run.states['node-1'].store.x.value, run.states['node-2'].store.x.value);
  run.fault({ kind: 'heal' }); run.advance(4000);
  for (const node of run.nodes) assert.equal(run.states[node].store.x.value, 'right');
});
test('token ring maintains mutual exclusion and halts on token holder crash', () => {
  const run = create('token'); run.advance(4500);
  const current = {};
  for (const e of run.events) if (e.type === 'state') {
    current[e.node] = e.state;
    assert.ok(Object.values(current).filter(s => s.hasToken).length <= 1);
  }
  const holder = run.nodes.find(n => run.states[n].hasToken);
  assert.ok(holder);
  run.fault({ kind: 'crash', node: holder });
  const seq = run.events.length; run.advance(5000);
  assert.equal(run.events.slice(seq).filter(e => e.type === 'send' && e.payload.type === 'Token').length, 0);
});
test('bandwidth serializes packets per directed link; reverse direction is independent', () => {
  const run = create('raft', { runtime: 'docker', latency: 10, bandwidth: 1 });
  const delivered = []; run.on('deliver', e => delivered.push({ ...e, at: run.time }));
  const payload = { data: 'x'.repeat(1024) };
  run.send('node-1', 'node-2', payload); run.send('node-1', 'node-2', payload); run.send('node-2', 'node-1', payload);
  run.advance(3000);
  assert.equal(delivered.length, 3);
  const forward = delivered.filter(e => e.from === 'node-1');
  assert.ok(forward[0].at >= 1010);
  assert.ok(forward[1].at - forward[0].at >= 1000);
  assert.equal(delivered.find(e => e.from === 'node-2').at, forward[0].at);
});
test('partition and crash invalidate in-flight packets even after recovery', () => {
  const run = create('raft', { runtime: 'docker', latency: 500 });
  let count = 0; run.on('deliver', () => count++);
  run.send('node-1', 'node-2', { type: 'Ping' });
  run.fault({ kind: 'crash', node: 'node-2' });
  run.fault({ kind: 'recover', node: 'node-2' });
  run.advance(1000);
  assert.equal(count, 0);
  assert.equal(run.events.filter(e => e.type === 'drop').length, 1);
  run.send('node-1', 'node-2', { type: 'Ping' });
  link(run, 'node-1', 'node-2', true); run.advance(1000);
  assert.equal(count, 0);
});
test('same seed and fault schedule reproduce exact simulation events', () => {
  const execute = () => {
    const run = create(); run.advance(3000); run.fault({ kind: 'crash', node: 'node-1' }); run.advance(3000);
    return run.events;
  };
  assert.deepEqual(execute(), execute());
});
test('validation rejects invalid runtime, topology, malformed faults and writes', () => {
  assert.throws(() => validateConfig({ nodeCount: 100 }));
  assert.throws(() => validateConfig({ runtime: 'remote' }));
  assert.throws(() => validateConfig({ protocol: 'custom', runtime: 'simulation' }));
  const run = create();
  assert.throws(() => link(run, 'node-1', 'node-1', true));
  assert.throws(() => link(run, 'node-1', 'node-2', true, { bandwidth: -1 }));
  assert.throws(() => run.command('node-1', 'x', 'y'));
  run.stop(); const length = run.events.length; run.advance(10000);
  assert.equal(run.events.length, length);
  assert.throws(() => run.fault({ kind: 'crash', node: 'node-1' }));
});
test('recovering an already online node does not duplicate its timers', () => {
  const a = create('gossip'), b = create('gossip');
  a.fault({ kind: 'recover', node: 'node-1' });
  a.advance(5000); b.advance(5000);
  assert.equal(a.events.filter(e => e.type === 'send').length, b.events.filter(e => e.type === 'send').length);
});
test('exponential jitter adds seeded Exp(mean) delay but keeps each link FIFO', () => {
  const run = new Experiment({ protocol: 'custom', runtime: 'docker', nodeCount: 2, latency: 100, bandwidth: 100000, delayModel: 'exponential', jitter: 200, seed: 7 });
  run.start();
  const arrivals = [];
  run.on('deliver', m => arrivals.push(m.payload.i));
  for (let i = 0; i < 400; i++) { run.send('node-1', 'node-2', { i }); run.advance(5); }
  run.advance(60000);
  assert.deepEqual(arrivals, [...arrivals].sort((a, b) => a - b));
  const jitters = run.events.filter(e => e.type === 'send').map(e => e.jitter);
  const mean = jitters.reduce((a, b) => a + b, 0) / jitters.length;
  assert.ok(mean > 150 && mean < 250, `mean jitter ${mean}`);
  assert.ok(jitters.every(j => Number.isInteger(j) && j >= 0));
  // A fixed link rule overrides the run's model; omitting the model inherits it.
  run.fault({ kind: 'link', from: 'node-1', to: 'node-2', latency: 50, bandwidth: 100000, blocked: false, bidirectional: false, delayModel: 'fixed', jitter: 0 });
  run.send('node-1', 'node-2', { i: 'fixed' });
  assert.equal(run.events.at(-1).delay >= 50, true);
  assert.equal(run.events.at(-1).jitter, undefined);
  assert.throws(() => validateConfig({ protocol: 'raft', delayModel: 'exponential', jitter: 0 }));
  assert.throws(() => validateConfig({ protocol: 'raft', delayModel: 'normal', jitter: 10 }));
  assert.equal(validateConfig({ protocol: 'raft' }).delayModel, 'fixed');
});
