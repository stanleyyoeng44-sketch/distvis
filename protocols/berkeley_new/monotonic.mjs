#!/usr/bin/env node
// Strict monotonicity check for the Berkeley protocol (lab §3.3C):
// during a gradual correction every node's software clock must keep moving
// FORWARD, including the node that is slowing down for a negative correction.
// The scenarios.json condition ops are point-in-time value comparisons, so
// this "never decreases over history" assertion lives here instead.
//
//   node protocols/berkeley_new/monotonic.mjs
//
// It runs one full round (offsets 0/+300/+600 -> deltas +300/0/-300) and
// samples all three clocks repeatedly while the slews are paying off.
const base = process.env.DISTVIS_URL || 'http://localhost:3000';
const FOLDER = 'berkeley_new';

async function api(path, body) {
  const res = await fetch(base + path, body === undefined ? {} : {
    method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body),
  });
  const data = await res.json();
  if (!res.ok) throw new Error(`${path} -> HTTP ${res.status}: ${data.error || res.statusText}`);
  return data;
}
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
async function waitFor(check, timeoutMs = 600000, what = 'condition') {
  const end = Date.now() + timeoutMs;
  while (Date.now() < end) {
    const value = await check();
    if (value) return value;
    await sleep(200);
  }
  throw new Error(`timeout waiting for ${what}`);
}

// sync the protocol from the directory and reuse (or create) one experiment
const project = await api('/api/project?protocol=' + encodeURIComponent('directory:' + FOLDER));
let protocol = (await api('/api/protocol-projects')).find((p) => !p.archived && p.origin?.directory === project.directory);
if (!protocol) protocol = await api('/api/protocol-projects', { name: project.name, protocol: 'directory:' + FOLDER });
else {
  const full = await api(`/api/protocol-projects/${protocol.id}`);
  if (full.projects?.shared?.sha256 !== project.sha256) {
    protocol = await api(`/api/protocol-projects/${protocol.id}`, {
      revision: full.revision,
      projects: { shared: { entry: project.entry, encoding: 'base64', files: project.files.map((f) => ({ path: f.path, content: f.content, encoding: f.encoding || 'base64' })) } },
    });
  }
}
let experiment = (await api(`/api/protocol-projects/${protocol.id}/experiments`)).find((e) => e.name === 'Berkeley · monotonicity' && !e.archived);
if (!experiment) experiment = await api('/api/experiments', {
  name: 'Berkeley · monotonicity', protocolId: protocol.id,
  settings: { runtime: 'docker', nodeCount: 3, latency: 80, bandwidth: 128, seed: 42 },
});

const run = await api('/api/runs', { experimentId: experiment.id, name: 'monotonicity check' });
const path = `/api/runs/${run.id}`;
console.log('RUN', run.id);

let nodes = {};
async function refresh() {
  const r = await api(path);
  nodes = {};
  for (const e of r.events) {
    if (!e.node) continue;
    const n = (nodes[e.node] ||= {});
    if (e.type === 'state') n.state = e.state;
    if (e.type === 'input_schema') n.schema = e.schema;
  }
  return r;
}
async function result(command) {
  return waitFor(async () => {
    const r = await refresh();
    return r.events.find((e) => e.type === 'command_result' && e.commandId === command.id);
  }, 30000, `result of ${command.id}`);
}
async function action(node, label, values) {
  const act = nodes[node].schema.find((a) => a.label === label);
  if (!act) throw new Error(`no action ${label} on ${node}`);
  const res = await result(await api(path + '/commands', { node, action: act.action, values }));
  if (res.error) throw new Error(`${node} ${label}: ${res.error}`);
  return res.result;
}
async function readAll() {
  const batch = ['node-1', 'node-2', 'node-3'].map((node) => ({
    node, action: nodes[node].schema.find((a) => a.label === 'Application.Read').action, values: {},
  }));
  const states = {};
  const res = await api(path + '/commands', { batch });
  await Promise.all(res.commands.map(async (c) => {
    const r = await result(c);
    if (!r.error) states[c.node] = r.result;
  }));
  return states;
}

let violations = 0;
try {
  await waitFor(async () => {
    const r = await refresh();
    return r.status === 'running' && ['node-1', 'node-2', 'node-3'].every((id) => nodes[id]?.schema) && r;
  }, 600000, 'run ready');

  // One round: node-1 slews +300 ms (runs fast), node-3 slews -300 ms (slows
  // to 89% of real rate: still forward, never backwards).
  await action('node-1', 'Application.ConfigureClock', { offsetMS: 0, driftPPM: 0 });
  await action('node-2', 'Application.ConfigureClock', { offsetMS: 300, driftPPM: 0 });
  await action('node-3', 'Application.ConfigureClock', { offsetMS: 600, driftPPM: 0 });
  await action('node-1', 'Application.PollPeers', {});
  const round = await action('node-1', 'Application.ComputeDeltas', { outlierThresholdMS: 1000 });
  await action('node-1', 'Application.SendAdjustments', {});
  console.log(`round #${round.number}: deltas ${round.samples.map((s) => `${s.node}=${s.deltaMS.toFixed(1)}`).join('  ')}`);
  console.log('sampling all three clocks every 300 ms while the corrections pay off:');

  let previous = {};
  let samples = 0;
  for (let tick = 0; tick <= 13; tick++) {
    const states = await readAll();
    samples++;
    const line = ['node-1', 'node-2', 'node-3'].map((id) => states[id].clock.timeMS.toFixed(1).padStart(15)).join('');
    const pend = ['node-1', 'node-2', 'node-3'].map((id) => states[id].clock.pendingMS.toFixed(1).padStart(8)).join('');
    console.log(`  t=${(tick * 0.3).toFixed(1).padStart(4)}s  timeMS:${line}  pending:${pend}`);
    for (const id of ['node-1', 'node-2', 'node-3']) {
      if (previous[id] !== undefined && !(states[id].clock.timeMS > previous[id])) {
        console.error(`  ✗ MONOTONICITY VIOLATED on ${id}: ${previous[id]} -> ${states[id].clock.timeMS}`);
        violations++;
      }
      previous[id] = states[id].clock.timeMS;
    }
    const done = ['node-1', 'node-2', 'node-3'].every((id) => Math.abs(states[id].clock.pendingMS) < 1);
    if (done) break;
    await sleep(300);
  }
  const final = await readAll();
  const spread = Math.max(...['node-1', 'node-2', 'node-3'].map((id) => final[id].clock.hostOffsetMS)) -
    Math.min(...['node-1', 'node-2', 'node-3'].map((id) => final[id].clock.hostOffsetMS));
  console.log(`\nfinal: hostOffset ${['node-1', 'node-2', 'node-3'].map((id) => id + '=' + final[id].clock.hostOffsetMS.toFixed(1)).join('  ')} (spread ${spread.toFixed(1)} ms)`);
  if (violations === 0) {
    console.log(`✓ PASS: ${samples} samples, every clock strictly increased on every node — a slowing clock still moves forward`);
  } else {
    console.log(`✗ FAIL: ${violations} monotonicity violations`);
    process.exitCode = 1;
  }
} catch (err) {
  console.error('FAILED:', err.message);
  process.exitCode = 1;
} finally {
  const current = await api(path).catch(() => null);
  if (current?.status === 'running') await api(path + '/stop', {}).catch(() => {});
  console.log('run stopped; history preserved for replay');
}
