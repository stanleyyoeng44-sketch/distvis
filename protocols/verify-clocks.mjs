// Real-Docker acceptance checks for the four Go lessons. This is JavaScript,
// not another Go package: it talks to DistVis's documented local HTTP API.
// Run from any directory with Node.js 22+:
//   node protocols/verify-clocks.mjs
// To preserve another run's report, choose a NEW filename beside this script:
//   node protocols/verify-clocks.mjs clock-verification-second.json
//
// Prerequisites: coordinator running, Docker ready, all four modules imported
// (and re-synchronized after any edit), and no unrelated active run. This
// script does not import/overwrite protocol source or stop someone else's run.
import assert from 'node:assert/strict'; // Throws if an observed invariant is false.
import { readFile, writeFile } from 'node:fs/promises'; // Promise-based filesystem APIs.
import { fileURLToPath } from 'node:url'; // Convert module URLs to local filesystem paths.
import { basename } from 'node:path';

const base = process.env.DISTVIS_URL ?? 'http://127.0.0.1:3000';
const reportName = process.argv[2] ?? 'clock-verification.json';
assert.equal(basename(reportName), reportName, 'report must stay directly under protocols');
assert.match(reportName, /^[a-zA-Z0-9._-]+\.json$/);
const output = new URL(reportName, import.meta.url);
// Inspect an existing target and refuse to overwrite it. Histories and even
// failed acceptance reports should remain available for honest comparison.
try {
  await readFile(output, 'utf8');
  throw new Error(`Report already exists: ${reportName}; choose a new filename`);
} catch (error) {
  if (error.code !== 'ENOENT') throw error;
}

const evidence = { startedAt: new Date().toISOString(), base, runtime: 'docker', runs: [] };
const sleep = ms => new Promise(resolve => setTimeout(resolve, ms));

// JSON requests contain no authentication secrets; this is the local lab API.
// A fetch success only means HTTP succeeded, NOT that a Go application action
// finished. command() below separately waits for its matching command_result.
async function api(path, body, acceptance = false) {
  const response = await fetch(base + path, {
    method: body === undefined ? 'GET' : 'POST',
    headers: { 'Content-Type': 'application/json', ...(acceptance ? { 'X-Distvis-Purpose': 'acceptance' } : {}) },
    body: body === undefined ? undefined : JSON.stringify(body),
    signal: AbortSignal.timeout(15000),
  });
  const value = await response.json();
  if (!response.ok) throw new Error(`${path}: HTTP ${response.status}: ${JSON.stringify(value)}`);
  return value;
}
async function waitFor(check, description, milliseconds = 20000) {
  const deadline = Date.now() + milliseconds;
  while (Date.now() < deadline) {
    const value = await check();
    if (value) return value;
    await sleep(200);
  }
  throw new Error(`Timed out waiting for ${description}`);
}
function approximately(actual, expected, tolerance, label) {
  assert.ok(Number.isFinite(actual) && Math.abs(actual - expected) <= tolerance,
    `${label}: got ${actual}, expected ${expected} +/- ${tolerance}`);
}

// One Run object knows the ID created by this script and its evidence record.
// Reading the full event list is fine for these small, bounded acceptance runs;
// an interactive long-running monitor should instead use SSE or MCP pagination.
class Run {
  constructor(id, record) { this.id = id; this.record = record; }
  inspect() { return api(`/api/runs/${this.id}`); }
  fault(value) { return api(`/api/runs/${this.id}/faults`, value); }
  async schema(node) {
    const run = await this.inspect();
    const event = run.events.findLast(e => e.type === 'input_schema' && e.node === node);
    assert.ok(event, `no live input schema for ${node}`);
    return event.schema;
  }
  async action(node, method) {
    const entry = (await this.schema(node)).find(s => s.label === `Application.${method}`);
    assert.ok(entry, `${node} did not declare Application.${method}`);
    return entry.action; // Never hard-code generated action IDs.
  }
  async result(node, method, id, expectError = false) {
    const event = await waitFor(async () => {
      const run = await this.inspect();
      const result = run.events.find(e => e.type === 'command_result' && e.commandId === id);
      if (result) return result;
      assert.equal(run.status, 'running', `run ended before ${id} completed`);
      return false;
    }, `${node} ${method} result ${id}`);
    this.record.commands.push({ node, method, commandId: id, seq: event.seq, error: event.error ?? null, result: event.result });
    if (expectError) assert.ok(event.error, `${method} should have failed`);
    else assert.ok(!event.error, `${node} ${method} failed: ${event.error}`);
    return expectError ? event : event.result;
  }
  async command(node, method, values = {}, expectError = false) {
    const action = await this.action(node, method);
    const submitted = await api(`/api/runs/${this.id}/commands`, { node, action, values });
    return this.result(node, method, submitted.id, expectError);
  }
  async concurrentLocal() {
    // The coordinator accepts both Local inputs at one coordinator instant.
    // Each command still has its own ID and its own completion event.
    const batch = await Promise.all([
      ['node-1', 'a'], ['node-3', 'e'],
    ].map(async ([node, label]) => ({ node, action: await this.action(node, 'Local'), values: { label } })));
    const submitted = await api(`/api/runs/${this.id}/commands`, { batch });
    assert.equal(submitted.commands.length, 2);
    return Promise.all(submitted.commands.map(c => {
      assert.ok(!c.error, c.error);
      return this.result(c.node, 'Local', c.id);
    }));
  }
  async configure(offsets) {
    // ConfigureClock is a laboratory disturbance, not a synchronization step.
    // Zero drift makes tolerances describe network noise, not accumulated drift.
    for (let i = 0; i < offsets.length; i++) {
      await this.command(`node-${i + 1}`, 'ConfigureClock', { offsetMS: offsets[i], driftPPM: 0 });
    }
  }
  read(node) { return this.command(node, 'Read'); }
  link(from, to, blocked, latency = 40, bidirectional = true) {
    return this.fault({ kind: 'link', from, to, blocked, latency, bandwidth: 1024, bidirectional });
  }
  checked(name, details) {
    this.record.checks.push({ name, details });
    console.log(`  PASS ${name}`);
  }
}

async function checkCristian(run) {
  await run.configure([0, 1200, 1500]);
  const sample = await run.command('node-2', 'Sync', { minDelayMS: 0 });
  approximately(sample.estimatedMS, sample.serverMS + sample.rttMS / 2, 0.001, 'Cristian midpoint');
  approximately(sample.correctionMS, sample.estimatedMS - sample.beforeMS, 0.001, 'Cristian delta');
  approximately(sample.uncertaintyMS, sample.rttMS / 2, 0.001, 'Cristian uncertainty');
  const after = await run.read('node-2');
  assert.ok(Math.abs(after.clock.hostOffsetMS) < 180, 'client did not approach reference');
  run.checked('RTT midpoint correction and reported uncertainty', { sample, observedOffsetMS: after.clock.hostOffsetMS });

  await run.command('node-2', 'ConfigureClock', { offsetMS: 600, driftPPM: 0 });
  await run.link('node-2', 'node-1', true);
  await run.command('node-2', 'Sync', { minDelayMS: 0 }, true);
  const failed = await run.read('node-2');
  approximately(failed.clock.hostOffsetMS, 600, 5, 'failed exchange must not adjust clock');
  assert.ok(!failed.busy && failed.lastError);
  await run.fault({ kind: 'heal' });
  run.checked('unavailable server leaves clock unchanged and clears busy', { observedOffsetMS: failed.clock.hostOffsetMS });

  // Deliberately asymmetric paths: the formulas cannot infer the true one-way
  // delay. Large separation makes the residual bias visible above 50-ms ticks.
  await run.command('node-2', 'ConfigureClock', { offsetMS: 1000, driftPPM: 0 });
  await run.link('node-2', 'node-1', false, 20, false);
  await run.link('node-1', 'node-2', false, 320, false);
  const asymmetric = await run.command('node-2', 'Sync', { minDelayMS: 0 });
  const biased = await run.read('node-2');
  assert.ok(biased.clock.hostOffsetMS < -50, 'expected negative bias on slower return path');
  run.checked('asymmetric path produces expected residual bias', { observedOffsetMS: biased.clock.hostOffsetMS, sample: asymmetric });
}

async function checkNTP(run) {
  await run.configure([0, 1200, -700]);
  await run.command('node-3', 'Sync', { servers: ['node-2'], samples: 1, processingMS: 0 }, true);
  assert.equal((await run.read('node-3')).stratum, 16);
  run.checked('unsynchronized upstream is rejected', {});

  const round = await run.command('node-2', 'Sync', { servers: ['node-1'], samples: 3, processingMS: 150 });
  const valid = round.samples.filter(s => !s.error);
  assert.equal(valid.length, 3);
  for (const sample of valid) {
    approximately(sample.delayMS, (sample.t3 - sample.t0) - (sample.t2 - sample.t1), 0.001, 'NTP delay');
    approximately(sample.offsetMS, ((sample.t1 - sample.t0) + (sample.t2 - sample.t3)) / 2, 0.001, 'NTP offset');
    assert.ok(sample.t2 - sample.t1 >= 140, 'server processing interval was not bracketed');
  }
  approximately(round.samples[round.selected].delayMS, Math.min(...valid.map(s => s.delayMS)), 0.001, 'minimum-delay selection');
  const middle = await run.read('node-2');
  assert.equal(middle.stratum, 2);
  assert.ok(Math.abs(middle.clock.hostOffsetMS) < 180);
  run.checked('four timestamps subtract server work and select minimum delay', { round, observedOffsetMS: middle.clock.hostOffsetMS });

  await run.command('node-3', 'Sync', { servers: ['node-2'], samples: 3, processingMS: 0 });
  const leaf = await run.read('node-3');
  assert.equal(leaf.stratum, 3);
  assert.equal(leaf.upstream, 'node-2');
  assert.ok(Math.abs(leaf.clock.hostOffsetMS) < 250);
  run.checked('hierarchy propagates reference through strata 1, 2, 3', { leaf });

  await run.link('node-3', 'node-2', true);
  const partial = await run.command('node-3', 'Sync', { servers: ['node-1', 'node-2'], samples: 2, processingMS: 0 });
  assert.equal(partial.samples[partial.selected].server, 'node-1');
  assert.ok(partial.samples.filter(s => s.server === 'node-2').every(s => s.error));
  assert.equal((await run.read('node-3')).stratum, 2);
  await run.fault({ kind: 'heal' });
  run.checked('failed upstream samples are excluded while another server succeeds', { partial });
}

async function checkBerkeley(run) {
  await run.configure([0, 120, 240]);
  const round = await run.command('node-1', 'Sync', { outlierThresholdMS: 1000 });
  assert.equal(round.samples.length, 3);
  for (const sample of round.samples) {
    assert.ok(sample.included && sample.scheduled && !sample.error);
    approximately(sample.deltaMS, round.meanMS - sample.offsetMS, 0.001, 'relative Berkeley correction');
  }
  const first = await run.read('node-3');
  assert.ok(first.clock.pendingMS < 0, 'negative adjustment should remain pending, not jump backwards');
  let states;
  await waitFor(async () => {
    states = await Promise.all(['node-1', 'node-2', 'node-3'].map(node => run.read(node)));
    return states.every(s => Math.abs(s.clock.pendingMS) < 0.001);
  }, 'Berkeley slews to complete', 12000);
  assert.ok(states[2].clock.timeMS > first.clock.timeMS, 'negative correction moved clock backwards');
  const offsets = states.map(s => s.clock.hostOffsetMS);
  assert.ok(Math.max(...offsets) - Math.min(...offsets) < 180, 'clocks did not converge near a common offset');
  for (const offset of offsets) approximately(offset, round.meanMS, 150, 'group mean, not forced host zero');
  run.checked('relative adjustments slew continuously toward group mean', { round, finalOffsetsMS: offsets });

  await run.configure([0, 100, 200]);
  await run.link('node-1', 'node-3', true);
  const partial = await run.command('node-1', 'Sync', { outlierThresholdMS: 500 });
  assert.ok(partial.samples[0].scheduled && partial.samples[1].scheduled);
  assert.ok(partial.samples[2].error && !partial.samples[2].scheduled);
  await run.fault({ kind: 'heal' });
  run.checked('unreachable participant is excluded without fabricating its offset', { partial });

  await run.configure([0, 100, 10000]);
  const filtered = await run.command('node-1', 'Sync', { outlierThresholdMS: 500 });
  assert.ok(filtered.samples[0].included && filtered.samples[1].included);
  assert.ok(!filtered.samples[2].included && filtered.samples[2].scheduled);
  assert.ok(filtered.samples[2].deltaMS < -9000);
  const outlier = await run.read('node-3');
  assert.ok(outlier.clock.pendingMS < -9000, 'outlier correction was stepped instead of scheduled');
  run.checked('outlier cannot bias mean but still receives a gradual correction', { filtered, outlier });
}

async function checkLogical(run) {
  await run.concurrentLocal();
  const first = await run.command('node-1', 'Send', { to: 'node-2', label: 'b', receiveLabel: 'c', payload: 'm1' });
  const second = await run.command('node-2', 'Send', { to: 'node-3', label: 'd', receiveLabel: 'f', payload: 'm2' });
  assert.ok(first.delivery.confirmed && second.delivery.confirmed);
  const states = await Promise.all(['node-1', 'node-2', 'node-3'].map(node => run.read(node)));
  const byLabel = Object.fromEntries(states.flatMap(s => s.events).map(e => [e.label, e]));
  const expected = {
    a: { lamport: 1, vector: [1, 0, 0], total: 3 },
    b: { lamport: 2, vector: [2, 0, 0], total: 6 },
    c: { lamport: 3, vector: [2, 1, 0], total: 10 },
    d: { lamport: 4, vector: [2, 2, 0], total: 13 },
    e: { lamport: 1, vector: [0, 0, 1], total: 5 },
    f: { lamport: 5, vector: [2, 2, 2], total: 17 },
  };
  for (const [label, timestamp] of Object.entries(expected)) assert.deepEqual(byLabel[label].timestamp, timestamp);
  run.checked('exact six-event lecture trace, including concurrent Local inputs', { events: byLabel });

  const before = await run.command('node-1', 'Compare', { left: [2, 0, 0], right: [2, 2, 2] });
  const concurrent = await run.command('node-1', 'Compare', { left: [2, 0, 0], right: [0, 0, 1] });
  const equal = await run.command('node-1', 'Compare', { left: [1, 0, 0], right: [1, 0, 0] });
  assert.equal(before.relation, 'happens-before');
  assert.equal(concurrent.relation, 'concurrent');
  assert.equal(equal.relation, 'equal');
  assert.deepEqual((await run.read('node-1')).clock, states[0].clock, 'observation must not increment clocks');
  run.checked('causality, concurrency and equality; no observer/ack increments', { before, concurrent, equal });

  const previous = await run.inspect();
  const lastSeq = previous.events.at(-1).seq;
  await run.fault({ kind: 'crash', node: 'node-2' });
  await run.fault({ kind: 'recover', node: 'node-2' });
  await waitFor(async () => (await run.inspect()).events.some(e => e.seq > lastSeq && e.type === 'input_schema' && e.node === 'node-2'),
    'recovered node to publish a fresh schema', 60000);
  const restored = await run.read('node-2');
  assert.deepEqual(restored.clock, states[1].clock);
  const next = await run.command('node-2', 'Local', { label: 'after recovery' });
  assert.equal(next.timestamp.lamport, 5);
  assert.deepEqual(next.timestamp.vector, [2, 3, 0]);
  assert.notEqual(next.id, byLabel.d.id);
  run.checked('crash/recover restores counters without reusing an event ID', { restoredClock: restored.clock, next });

  // Drop only the RETURN path: receive executes, but its acknowledgement is
  // lost. This proves why the sender must say "unconfirmed", not "not delivered".
  await run.link('node-2', 'node-1', true, 40, false);
  const uncertain = await run.command('node-1', 'Send', {
    to: 'node-2', label: 'ack-lost-send', receiveLabel: 'ack-lost-receive', payload: 'one-way test',
  });
  assert.ok(!uncertain.delivery.confirmed && uncertain.delivery.error);
  const receiver = await run.read('node-2');
  assert.ok(receiver.events.some(e => e.messageID === uncertain.event.messageID && e.label === 'ack-lost-receive'));
  await run.fault({ kind: 'heal' });
  run.checked('lost acknowledgement leaves durable receive and unconfirmed send', { uncertain, receiverClock: receiver.clock });
}

const lessons = [
  ['cristian', checkCristian], ['ntp', checkNTP], ['berkeley', checkBerkeley], ['logical-clocks', checkLogical],
];
try {
  const projects = await api('/api/protocol-projects');
  for (const [folder, verify] of lessons) {
    const directory = fileURLToPath(new URL(`${folder}/`, import.meta.url)).replace(/\/$/, '');
    const matches = projects.filter(p => p.origin?.directory === directory);
    assert.equal(matches.length, 1, `import exactly one current snapshot of ${folder} before running acceptance`);
    const protocol = matches[0];
    const existing = await api(`/api/protocol-projects/${protocol.id}/experiments`);
    const name = `Clock acceptance · ${folder}`;
    let experiment = existing.find(e => e.name === name && e.archived);
    if (!experiment) {
      experiment = await api('/api/experiments', {
        protocolId: protocol.id, name,
        settings: { runtime: 'docker', nodeCount: 3, latency: 40, bandwidth: 1024, seed: 42, delayModel: 'fixed' },
      }, true);
    }
    const active = (await api('/api/runs')).filter(r => ['starting', 'running'].includes(r.status));
    assert.equal(active.length, 0, 'another run is active; refusing to interrupt it');
    const started = await api('/api/runs', { experimentId: experiment.id, name: `Clock acceptance: ${folder}` });
    const record = { folder, protocolId: protocol.id, revision: protocol.revision, experimentId: experiment.id,
      runId: started.id, checks: [], commands: [], passed: false };
    evidence.runs.push(record);
    const run = new Run(started.id, record);
    console.log(`Testing ${folder}: ${started.id}`);
    try {
      await waitFor(async () => {
        const current = await run.inspect();
        if (['failed', 'completed', 'interrupted'].includes(current.status)) {
          throw new Error(`Startup ended as ${current.status}: ${JSON.stringify(current.events.filter(e => e.type === 'runtime'))}`);
        }
        return current.status === 'running' && ['node-1', 'node-2', 'node-3'].every(node =>
          current.events.some(e => e.type === 'input_schema' && e.node === node));
      }, `${folder} Docker build and all node schemas`, 600000);
      await verify(run);
      const complete = await run.inspect();
      assert.ok(complete.events.some(e => e.type === 'send' && e.payload?.type === 'RPCRequest'), 'no real RPC traffic recorded');
      record.eventCount = complete.events.length;
      record.passed = true;
    } finally {
      // Ownership boundary: this ID was returned by OUR start request. Never
      // stop a run found merely by listing the platform's active runs.
      const current = await run.inspect();
      if (current.status === 'running') await api(`/api/runs/${run.id}/stop`, {});
      else if (current.status === 'starting') console.error(`Run ${run.id} is still starting and cannot be stopped yet.`);
      record.finalStatus = (await run.inspect()).status;
    }
  }
} catch (error) {
  evidence.error = error.stack ?? String(error);
  process.exitCode = 1; // Finish the report/cleanup, then exit nonzero for automation.
  console.error(evidence.error);
} finally {
  evidence.finishedAt = new Date().toISOString();
  evidence.passed = evidence.runs.length === lessons.length && evidence.runs.every(r => r.passed) && !evidence.error;
  // wx creates a NEW file exclusively; it cannot clobber an existing report.
  await writeFile(output, JSON.stringify(evidence, null, 2) + '\n', { flag: 'wx' });
  console.log(`Evidence: ${fileURLToPath(output)} (passed=${evidence.passed})`);
}
