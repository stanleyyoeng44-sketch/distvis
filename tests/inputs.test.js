import { test } from 'node:test';
import assert from 'node:assert/strict';
import { Experiment } from '../server/engine.js';
import { validateSchema } from '../server/inputs.js';
const create = protocol => {
  const run = new Experiment({ protocol, runtime: protocol === 'custom' ? 'docker' : 'simulation', nodeCount: 3 });
  run.start(); return run;
};
test('arbitrary per-node input declarations validate and transport typed values unchanged', () => {
  const run = create('custom'), received = [];
  run.on('command', cmd => received.push(cmd));
  const schema = [{ action: 'launch', label: 'Launch', fields: [
    { name: 'count', label: 'Count', type: 'number', required: true, integer: true, min: 0, max: 9 },
    { name: 'enabled', label: 'Enabled', type: 'boolean', required: true },
    { name: 'mode', label: 'Mode', type: 'select', options: ['a', 'b'] },
    { name: 'payload', label: 'Payload', type: 'json', required: true }
  ] }];
  run.declareInputs('node-1', schema);
  const values = { count: 0, enabled: false, mode: 'a', payload: { nested: [1, null, '文'] } };
  const result = run.command('node-1', undefined, undefined, 'launch', values);
  assert.deepEqual(received[0].values, values);
  assert.equal(received[0].id, result.id);
  assert.deepEqual(run.events.find(e => e.seq === result.commandSeq).values, values);
  for (const bad of [{ ...values, count: '' }, { ...values, count: 1.5 }, { ...values, count: 10 }, { ...values, mode: 'c' }, { ...values, enabled: 'false' }, { ...values, unknown: 1 }]) {
    assert.throws(() => run.command('node-1', undefined, undefined, 'launch', bad));
  }
  assert.throws(() => run.command('node-2', undefined, undefined, 'launch', values));
  assert.throws(() => run.command('node-1', undefined, undefined, 'other', values));
  assert.throws(() => run.command('node-1', undefined, undefined, 'launch', null));
  run.fault({ kind: 'crash', node: 'node-1' });
  assert.throws(() => run.command('node-1', undefined, undefined, 'launch', values));
  assert.equal(received.length, 1);
});
test('node and nodes fields accept only node IDs of the run', () => {
  const run = create('custom'), received = [];
  run.on('command', cmd => received.push(cmd));
  run.declareInputs('node-1', [{ action: 'send', label: 'Send', fields: [
    { name: 'to', label: 'To', type: 'nodes', required: true },
    { name: 'via', label: 'Via', type: 'node' }
  ] }]);
  run.command('node-1', undefined, undefined, 'send', { to: ['node-2', 'node-3'], via: 'node-2' });
  run.command('node-1', undefined, undefined, 'send', { to: ['node-3'], via: '' });
  assert.deepEqual(received.map(c => c.values.to), [['node-2', 'node-3'], ['node-3']]);
  for (const bad of [{ to: 'node-2' }, { to: [] }, { to: ['node-9'] }, { to: ['node-2', 'node-2'] }, { to: [2] }, { to: ['node-2'], via: 'node-9' }, { to: ['node-2'], via: 3 }]) {
    assert.throws(() => run.command('node-1', undefined, undefined, 'send', bad), undefined, JSON.stringify(bad));
  }
  assert.equal(received.length, 2);
});
test('malformed declarations are rejected before publication', () => {
  for (const schema of [null, {}, [{ fields: [] }], [{ action: 'test', label: 'Test', fields: [{ label: 'x', type: 'text' }] }],
    [{ action: 'test', label: 'Test', fields: [{ name: 'x', label: 'x', type: 'script' }] }]]) assert.throws(() => validateSchema(schema));
});
test('token application input cannot mint a token, waits for possession and appears on wire', () => {
  const run = create('token');
  run.command('node-3', undefined, undefined, 'enqueue', { payload: 'hello from three' });
  assert.equal(run.states['node-3'].hasToken, false);
  assert.equal(run.states['node-3'].pendingInput.payload, 'hello from three');
  run.advance(5000);
  assert.equal(run.states['node-3'].lastProcessedInput.payload, 'hello from three');
  assert.ok(run.events.some(e => e.type === 'receive' && e.payload.type === 'Token' && e.payload.payload?.payload === 'hello from three'));
  const states = {};
  for (const e of run.events) if (e.type === 'state') {
    states[e.node] = e.state;
    assert.ok(Object.values(states).filter(s => s.hasToken).length <= 1);
  }
});
test('Raft input submitted before election is forwarded to leader, without claiming commit', () => {
  const run = create('raft');
  run.command('node-3', undefined, undefined, 'propose', { number: 0 });
  run.advance(5000);
  const leader = run.nodes.find(n => run.states[n].role === 'leader');
  assert.equal(run.states[leader].lastReceivedProposal.number, 0);
  assert.equal(run.states['node-3'].lastProposal.status, 'Leader 已接收（未提交）');
  assert.equal(run.states[leader].commitIndex, undefined);
});
test('Raft proposal crosses the configured link and remains pending during partition', () => {
  const run = create('raft'); run.advance(3000);
  const leader = run.nodes.find(n => run.states[n].role === 'leader');
  const sender = run.nodes.find(n => n !== leader);
  for (const to of run.nodes.filter(n => n !== sender)) run.fault({ kind: 'link', from: sender, to, blocked: true, bidirectional: true, latency: 80, bandwidth: 128 });
  run.command(sender, undefined, undefined, 'propose', { number: 123 });
  run.advance(500);
  assert.equal(run.states[sender].lastProposal.status, '等待 Leader 接收');
  assert.ok(run.events.some(e => e.type === 'drop' && e.payload.type === 'Proposal'));
  run.fault({ kind: 'heal' }); run.advance(2500);
  assert.equal(run.states[sender].lastProposal.status, 'Leader 已接收（未提交）');
});
test('batched inputs reach every node at the same coordinator time, validated before any is sent', () => {
  const run = create('custom'), received = [];
  run.on('command', cmd => received.push(cmd));
  const schema = [{ action: 'go', label: 'Application.Go', fields: [{ name: 'n', label: 'n', type: 'number', required: true }] }];
  for (const node of run.nodes) run.declareInputs(node, schema);
  run.advance(120);
  const result = run.commandBatch(run.nodes.map(node => ({ node, action: 'go', values: { n: 7 } })));
  assert.deepEqual(received.map(c => c.node), run.nodes);
  assert.ok(result.commands.every(c => c.id && !c.error));
  const events = run.events.filter(e => e.type === 'command');
  assert.equal(new Set(events.map(e => e.time)).size, 1);
  assert.ok(events.every(e => e.batch === result.batch && e.concurrentWith.length === 2));
  // One invalid entry rejects the whole batch: nothing is logged or delivered.
  assert.throws(() => run.commandBatch([{ node: 'node-1', action: 'go', values: { n: 1 } }, { node: 'node-2', action: 'go', values: { n: 'x' } }]));
  assert.throws(() => run.commandBatch([{ node: 'node-1', action: 'go', values: { n: 1 } }, { node: 'node-1', action: 'go', values: { n: 2 } }]));
  assert.throws(() => run.commandBatch([]));
  assert.equal(received.length, 3);
});
