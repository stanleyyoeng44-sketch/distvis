#!/usr/bin/env node
// Export completed runs created for this protocol and independently check the
// recorded histories. Does not create, stop, or modify any platform run.
import { readFileSync, mkdirSync, writeFileSync } from 'node:fs';
import { dirname, resolve, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import assert from 'node:assert/strict';

const root = resolve(dirname(fileURLToPath(import.meta.url)), '..');
const base = process.env.DISTVIS_URL || 'http://127.0.0.1:3000';
const protocolId = process.argv[2];
if (!protocolId) throw new Error('Usage: node scripts/export-lamport-evidence.mjs <protocolId>');
const spec = JSON.parse(readFileSync(join(root, 'protocols/lamport_mutex/scenarios.json'), 'utf8'));
async function api(path) {
  const response = await fetch(base + path, { signal: AbortSignal.timeout(30000) });
  if (!response.ok) throw new Error(`${response.status}: ${await response.text()}`);
  return response.json();
}
const experiments = await api(`/api/protocol-projects/${protocolId}/experiments`);
const summary = [];
for (const scenario of spec.scenarios) {
  const experiment = experiments.find(e => e.name === scenario.name);
  assert(experiment, `Missing experiment ${scenario.name}`);
  const runs = await api(`/api/experiments/${experiment.id}/runs`);
  // Require the latest run to be completed; never silently choose an old pass.
  const latest = runs.sort((a, b) => b.createdAt.localeCompare(a.createdAt))[0];
  assert.equal(latest?.status, 'completed', `Latest run for ${scenario.id} is not completed`);
  const run = await api(`/api/runs/${latest.id}/export`);
  assert.equal(run.config.runtime, 'docker');
  assert.equal(run.config.nodeCount, 3);
  const sources = run.project.files.filter(f => f.path.endsWith('.go'));
  for (const f of sources) {
    assert(Buffer.from(f.content, f.encoding || run.project.encoding || 'utf8')
      .equals(readFileSync(join(root, 'protocols/lamport_mutex', f.path))), `Source mismatch: ${f.path}`);
  }
  const states = {}, online = {}, sequences = {}, criticalEntries = [], exits = [];
  const methods = {}, actualSent = {}, commands = new Map();
  let maxCritical = 0;
  const acceptanceMs = [];
  for (const e of run.events) {
    if (e.type === 'node') online[e.node] = e.online;
    if (e.type === 'command') commands.set(`input-${e.seq}`, e);
    if (e.type === 'command_result') {
      assert(!e.error, `${scenario.id}: application error ${e.error}`);
      const command = commands.get(e.commandId);
      assert(command, 'Unmatched application result');
      acceptanceMs.push(e.time - command.time);
      commands.delete(e.commandId);
    }
    if (e.type === 'state') {
      assert(e.state.reportSeq > (sequences[e.node] || 0), `${scenario.id}: out-of-order report`);
      sequences[e.node] = e.state.reportSeq;
      if (e.state.role === 'critical' && states[e.node]?.role !== 'critical') {
        criticalEntries.push({ node: e.node, seq: e.seq, time: e.time, requestTS: e.state.myTS });
      }
      if (e.state.role !== 'critical' && states[e.node]?.role === 'critical') {
        exits.push({ node: e.node, seq: e.seq, time: e.time });
      }
      states[e.node] = e.state;
      maxCritical = Math.max(maxCritical, Object.values(states)
        .filter(s => s.role === 'critical' && online[s.node] !== false).length);
    }
    if (e.type === 'send' && e.payload?.type === 'RPCRequest' && e.payload.method.startsWith('MutexService.')) {
      const method = e.payload.method;
      methods[method] = (methods[method] || 0) + 1;
      actualSent[e.from] = (actualSent[e.from] || 0) + 1;
    }
  }
  assert.equal(commands.size, 0, 'Application input missing a result');
  assert(maxCritical <= 1, `${scenario.id}: overlapping critical sections`);
  for (const [node, state] of Object.entries(states)) {
    assert.equal(state.messagesSent, actualSent[node] || 0, `${scenario.id}: stale send counter on ${node}`);
  }
  const total = Object.values(methods).reduce((a, b) => a + b, 0);
  const expected = { normal: 12, crash: 3, partition: 3, 'lamport-count': 6, 'ricart-count': 4, 'long-hold': 6, 'ricart-contention': 36 };
  assert.equal(total, expected[scenario.id], `${scenario.id}: unexpected message count`);
  if (['crash', 'partition'].includes(scenario.id)) {
    assert.equal(states['node-2'].entries, 0);
    assert.deepEqual(states['node-2'].missingAcks, ['node-3']);
    assert.match(states['node-2'].lastError, /node-3/);
    assert(states['node-2'].sendErrors >= 1);
    // A runtime may emit "stopped" while shutting down. Inspect the last state
    // before the stop event as well as checking that no entry ever occurred.
    assert.equal(criticalEntries.length, 0);
  } else {
    for (const s of Object.values(states)) {
      assert.equal(s.sendErrors, 0);
      assert.equal(s.role, 'idle');
      assert.equal(s.operationRunning, false);
      assert.equal(s.autoRunning, false);
      assert.deepEqual(s.queue, []);
    }
  }
  if (scenario.id === 'normal') {
    const first = [...criticalEntries].sort((a, b) => a.requestTS - b.requestTS || Number(a.node.slice(5)) - Number(b.node.slice(5)))[0];
    assert.equal(criticalEntries[0].node, first.node, 'Timestamp priority not respected');
    assert.equal(states['node-1'].completed, 1);
    assert.equal(states['node-3'].completed, 1);
  }
  if (scenario.id === 'long-hold') {
    assert(acceptanceMs[0] < 3000, 'Acquire did not return promptly');
    assert(exits[0].time - criticalEntries[0].time >= 14900, 'Critical-section hold ended early');
  }
  if (scenario.id.startsWith('ricart-')) assert.equal(methods['MutexService.Release'] || 0, 0);
  if (scenario.id === 'ricart-contention') for (const s of Object.values(states)) assert.equal(s.completed, 3);
  const directory = join(root, 'report/lamport_mutex/runs', run.id);
  // Each export is kept in its own new directory; existing evidence is never overwritten.
  mkdirSync(directory, { recursive: true });
  writeFileSync(join(directory, 'run.json'), JSON.stringify(run), { flag: 'wx' });
  summary.push({
    scenario: scenario.id, name: scenario.name, runId: run.id, experimentId: experiment.id,
    createdAt: run.createdAt, protocolRevision: run.config.protocolRevision,
    sourceSHA256: run.config.project.sha256, status: run.status,
    events: run.events.length, durationMs: run.time, algorithmMessages: total, methods,
    maxCritical, acceptanceMs, criticalEntries, exits,
    faults: run.events.filter(e => e.type === 'fault').map(e => ({ seq: e.seq, time: e.time, fault: e.fault })),
    states, export: `runs/${run.id}/run.json`, history: `${base}/#experiment/${experiment.id}/history`,
  });
  console.log(`${scenario.id}: verified and exported ${run.id}; ${total} algorithm messages`);
}
const output = join(root, 'report/lamport_mutex', `summary-${summary[0].runId}.json`);
writeFileSync(output, JSON.stringify({ protocolId, scenarios: summary }, null, 2), { flag: 'wx' });
console.log(`Summary: ${output}`);
