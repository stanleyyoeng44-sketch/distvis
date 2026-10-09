// Actual container acceptance against an idle local coordinator (npm start).
// Creates archived experiments and cleans up only the resources they own.
import assert from 'node:assert/strict';
import { execFile } from 'node:child_process';
import { promisify } from 'node:util';
import { mkdir, writeFile } from 'node:fs/promises';
const exec = promisify(execFile);
const base = process.env.DISTVIS_URL || 'http://127.0.0.1:3000';
const runtime = process.env.DISTVIS_RUNTIME || 'docker';
assert.ok(['docker', 'kubernetes'].includes(runtime));
const results = [];
async function api(path, data) {
  const response = await fetch(base + path, {
    ...(data === undefined ? {} : { method: 'POST', headers: { 'Content-Type': 'application/json', 'X-Distvis-Purpose': 'acceptance' }, body: JSON.stringify(data) }),
    signal: AbortSignal.timeout(15000),
  });
  const value = await response.json();
  if (!response.ok) throw new Error(value.error);
  return value;
}
async function waitFor(check, description, timeout = 20000) {
  const end = Date.now() + timeout;
  while (Date.now() < end) {
    const result = await check();
    if (result) return result;
    await new Promise(r => setTimeout(r, 250));
  }
  throw new Error(`Timed out: ${description}`);
}
function snapshot(run) {
  const states = {}, online = Object.fromEntries(Array.from({ length: run.config.nodeCount }, (_, i) => [`node-${i + 1}`, true]));
  for (const event of run.events) {
    if (event.type === 'state') states[event.node] = event.state;
    if (event.type === 'node') online[event.node] = event.online;
  }
  return { states, online, leaders: Object.keys(states).filter(n => online[n] && states[n].role === 'leader') };
}
async function assertResources(id, count) {
  if (runtime === 'docker') {
    const { stdout } = await exec('docker', ['ps', '-a', '--filter', `label=distvis.run=${id}`, '--format', '{{.Names}}']);
    assert.equal(stdout.trim().split('\n').filter(Boolean).length, count);
  } else {
    const { stdout } = await exec('kubectl', ['--context', 'docker-desktop', 'get', 'pods', '-n', `distvis-${id.slice(0, 8)}`, '-o', 'json']);
    assert.equal(JSON.parse(stdout).items.length, count);
  }
}
assert.equal((await api('/api/runs')).filter(r => ['running', 'starting'].includes(r.status)).length, 0, 'coordinator must be idle');
for (const protocol of ['raft', 'token', 'gossip']) {
  const created = await api('/api/runs', { name: `容器验收 · ${runtime} · ${protocol}`, runtime, protocol, nodeCount: 5, latency: 80, bandwidth: 128 });
  const id = created.id, path = `/api/runs/${id}`;
  console.log(`BUILD ${protocol}: ${id}`);
  try {
    await waitFor(async () => {
      const run = await api(path);
      if (run.status === 'failed') throw new Error(run.events.find(e => e.level === 'error')?.message || 'Startup failed');
      return run.status === 'running';
    }, 'image build and node startup', 360000);
    await assertResources(id, 5);
    await waitFor(async () => Object.keys(snapshot(await api(path)).states).length === 5, 'all Go nodes report state');
    const command = async (node, values) => {
      const current = await api(path);
      const schema = current.events.filter(e => e.type === 'input_schema' && e.node === node).at(-1)?.schema;
      assert.equal(schema?.length, 1, 'one inferred application method');
      return api(`${path}/commands`, { node, action: schema[0].action, values });
    };
    if (protocol === 'raft') {
      const initial = await waitFor(async () => {
        const s = snapshot(await api(path)); return s.leaders.length === 1 && s;
      }, 'Raft leader');
      const leader = initial.leaders[0], term = initial.states[leader].term;
      const proposer = Object.keys(initial.states).find(n => n !== leader);
      await command(proposer, { number: 42 });
      await waitFor(async () => snapshot(await api(path)).states[proposer]?.lastProposal?.status === 'Leader 已接收（未提交）', 'proposal forwarded from follower to leader');
      assert.equal(snapshot(await api(path)).states[leader].lastReceivedProposal.number, 42);
      await api(`${path}/faults`, { kind: 'crash', node: leader });
      if (runtime === 'docker') {
        const { stdout } = await exec('docker', ['inspect', '--format', '{{.State.Running}}', `distvis-${id.slice(0, 8)}-${leader}`]);
        assert.equal(stdout.trim(), 'false', 'crash must stop the actual container');
      }
      const elected = await waitFor(async () => {
        const s = snapshot(await api(path)); return s.leaders.length === 1 && s.leaders[0] !== leader && s;
      }, 'leader reelection');
      assert.ok(elected.states[elected.leaders[0]].term > term);
      await api(`${path}/faults`, { kind: 'recover', node: leader });
      await waitFor(async () => {
        const s = snapshot(await api(path));
        return s.online[leader] && s.states[leader].role === 'follower' && s.states[leader].term > term && s.leaders.length === 1;
      }, 'crashed node recovers and catches up');
    } else if (protocol === 'token') {
      await command('node-5', { Payload: 'token-application-input' });
      await waitFor(async () => snapshot(await api(path)).states['node-5']?.lastProcessed === 'token-application-input', 'payload waits for token');
      await waitFor(async () => (await api(path)).events.some(e => e.type === 'receive' && e.payload.method === 'Ring.Pass' && e.payload.body?.Payload === 'token-application-input'), 'payload appears in token message');
      await waitFor(async () => Object.values(snapshot(await api(path)).states).every(s => s.entries > 0), 'token visits every node');
      const states = {};
      for (const event of (await api(path)).events) if (event.type === 'state') {
        states[event.node] = event.state;
        assert.ok(Object.values(states).filter(s => s.hasToken).length <= 1, 'mutual exclusion');
      }
    } else {
      await waitFor(async () => Object.values(snapshot(await api(path)).states).every(s => s.store?.course), 'initial replicas converge');
      for (let i = 2; i <= 5; i++) await api(`${path}/faults`, { kind: 'link', from: 'node-1', to: `node-${i}`, blocked: true, bidirectional: true, latency: 80, bandwidth: 128 });
      await command('node-1', { key: 'answer', value: 'left' });
      await command('node-2', { key: 'answer', value: 'right' });
      await waitFor(async () => {
        const { states } = snapshot(await api(path));
        return states['node-1'].store.answer?.value === 'left' && states['node-2'].store.answer?.value === 'right';
      }, 'partitioned conflicting writes');
      await api(`${path}/faults`, { kind: 'heal' });
      await waitFor(async () => Object.values(snapshot(await api(path)).states).every(s => s.store.answer?.value === 'right'), 'replicas converge after healing');
    }
    const run = await api(path);
    assert.ok(run.events.some(e => e.type === 'receive'), 'actual SDK Receive acknowledgements');
    assert.ok(run.events.some(e => e.type === 'deliver'), 'actual process inbox delivery');
    assert.equal(run.events.filter(e => e.type === 'stderr').length, 0, 'no program/protocol errors');
    await api(`${path}/stop`, {});
    const archive = await api(`${path}/export`);
    assert.equal(archive.status, 'completed');
    assert.match(Buffer.from(archive.project.files.find(f => f.path === 'main.go').content, 'base64').toString(), /lab.Main/);
    assert.ok(archive.project.files.some(f => f.path === 'protocol.go'));
    assert.ok(archive.events.some(e => e.type === 'command_result' && !e.error));
    assert.ok(archive.events.some(e => e.type === 'lifecycle' && e.action === 'stop'));
    assert.ok(archive.events.some(e => e.type === 'command'));
    assert.equal(new Set(archive.events.filter(e => e.type === 'input_schema').map(e => e.node)).size, 5);
    results.push({ protocol, runtime, id, events: archive.events.length, status: 'passed' });
    console.log(`PASS ${protocol}: ${archive.events.length} events`);
  } finally {
    const run = await api(path);
    if (run.status === 'running') await api(`${path}/stop`, {});
    await waitFor(async () => {
      if (runtime === 'docker') {
        const { stdout } = await exec('docker', ['ps', '-a', '--filter', `label=distvis.run=${id}`, '--format', '{{.Names}}']);
        return !stdout.trim();
      }
      const { stdout } = await exec('kubectl', ['--context', 'docker-desktop', 'get', 'namespace', `distvis-${id.slice(0, 8)}`, '--ignore-not-found', '-o', 'name']);
      return !stdout.trim();
    }, 'experiment container/namespace cleanup', 90000);
    await waitFor(async () => {
      const { stdout } = await exec('docker', ['image', 'ls', '--format', '{{.Repository}}:{{.Tag}}', `distvis-${id.slice(0, 8)}:local`]);
      return !stdout.trim();
    }, 'experiment image cleanup', 20000);
  }
}
await mkdir('artifacts', { recursive: true });
await writeFile(`artifacts/${runtime}-acceptance.json`, JSON.stringify({ checkedAt: new Date().toISOString(), results }, null, 2));
console.log(`All ${runtime} checks passed; experiments are available in history.`);
