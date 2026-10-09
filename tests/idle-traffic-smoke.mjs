// Idle-traffic acceptance. Each directory protocol either sends RPC messages on its own after
// start (no input, no faults), or its Go header has `// distvis:start node method {args} note`
// naming the input that begins traffic. This test sends exactly that input and requires the
// messages to follow, so the instruction shown in the UI is proven correct. Run against an idle
// coordinator (npm start) with Docker or K8s.
//
//   node tests/idle-traffic-smoke.mjs                 # all protocols under protocols/
//   node tests/idle-traffic-smoke.mjs clock-ntp ...   # selected folders
//   DISTVIS_IDLE_REPORT=1 node tests/idle-traffic-smoke.mjs   # measure only, never fail
import assert from 'node:assert/strict';
import { readdirSync, existsSync, readFileSync } from 'node:fs';
import { join, dirname } from 'node:path';
import { fileURLToPath } from 'node:url';

const base = process.env.DISTVIS_URL || 'http://localhost:3000';
const runtime = process.env.DISTVIS_RUNTIME || 'docker';
const windowMs = Number(process.env.DISTVIS_IDLE_WINDOW_MS || 15000);
const minMessages = Number(process.env.DISTVIS_IDLE_MIN || 3);
const reportOnly = process.env.DISTVIS_IDLE_REPORT === '1';
const root = join(dirname(fileURLToPath(import.meta.url)), '..', 'protocols');

async function api(path, data) {
  const r = await fetch(base + path, data === undefined ? {} : {
    method: 'POST', headers: { 'Content-Type': 'application/json', 'X-Distvis-Purpose': 'acceptance' }, body: JSON.stringify(data),
  });
  const body = await r.json();
  assert.ok(r.ok, `${path}: ${JSON.stringify(body)}`);
  return body;
}
const sleep = ms => new Promise(r => setTimeout(r, ms));

// Use the node count of the protocol's first scenario, so topology-dependent protocols
// (stratum layout, master/slave, coordinator) start in the shape they were written for.
function nodeCountOf(folder) {
  const file = join(root, folder, 'scenarios.json');
  if (!existsSync(file)) return 3;
  const first = JSON.parse(readFileSync(file, 'utf8')).scenarios?.[0];
  return first?.settings?.nodeCount ?? 5;
}

async function probe(folder, entry) {
  const nodeCount = nodeCountOf(folder);
  const run = await api('/api/runs', { protocol: entry.id, runtime, nodeCount, latency: 80, bandwidth: 128, seed: 42, name: `空闲流量验收 · ${folder}` });
  const path = `/api/runs/${run.id}`;
  const rpcSends = v => v.events.filter(e => e.type === 'send' && e.payload?.type === 'RPCRequest');
  try {
    let view;
    for (const end = Date.now() + 600000; ; await sleep(500)) {
      view = await api(path + '?summary');
      if (view.status === 'running') break;
      if (view.status !== 'starting' || Date.now() > end) {
        const full = await api(path);
        throw new Error(`${view.status}: ${full.events.find(e => e.level === 'error')?.message || 'not running'}`);
      }
    }
    await sleep(windowMs);
    view = await api(path);
    const idle = rpcSends(view), methods = {};
    for (const e of idle) methods[e.payload.method] = (methods[e.payload.method] || 0) + 1;
    const result = { folder, nodeCount, runId: run.id, messages: idle.length, senders: new Set(idle.map(e => e.from)).size, methods,
      commands: view.events.filter(e => e.type === 'command').length, start: entry.start };
    // A silent protocol passes only if its distvis:start comment names an input that really starts traffic.
    if (idle.length < minMessages && entry.start) {
      const s = entry.start, schema = view.events.filter(e => e.type === 'input_schema' && e.node === s.node).at(-1)?.schema || [];
      const action = schema.find(a => a.label === s.method)?.action;
      if (!action) throw new Error(`distvis:start 指向 ${s.node} ${s.method}，但该节点没有声明这个输入（有：${schema.map(a => a.label).join(', ') || '无'}）`);
      const before = view.events.length;
      const input = await api(path + '/commands', { node: s.node, action, values: s.values });
      for (const end = Date.now() + 20000; Date.now() < end; await sleep(500)) {
        view = await api(path);
        const reply = view.events.find(e => e.type === 'command_result' && e.commandId === input.id);
        const sent = rpcSends({ events: view.events.slice(before) }).length;
        if (reply?.error) throw new Error(`distvis:start 输入 ${s.method} 返回错误：${reply.error}`);
        // One request is enough: Cristian's Sync is a single Time.Now call by design.
        if (reply && sent >= 1) { result.started = sent; break; }
      }
      if (!result.started) throw new Error(`distvis:start 输入 ${s.node} ${s.method} 发出后 20s 内没有产生任何 RPC 消息`);
    }
    return result;
  } finally {
    await api(path + '/stop', {}).catch(() => {});
  }
}

if ((await api('/api/runs')).some(r => ['running', 'starting'].includes(r.status))) throw new Error('coordinator must be idle');
const library = await api('/api/library');
assert.equal(library.errors.length, 0, JSON.stringify(library.errors));
const wanted = process.argv.slice(2);
const folders = (wanted.length ? wanted : readdirSync(root).filter(f => existsSync(join(root, f, 'main.go')))).sort();

const results = [];
for (const folder of folders) {
  const entry = library.entries.find(e => e.folder === folder);
  if (!entry) { results.push({ folder, error: 'not in library' }); continue; }
  try { results.push(await probe(folder, entry)); }
  catch (e) { results.push({ folder, error: e.message }); }
  const r = results.at(-1);
  const auto = r.messages >= minMessages;
  console.log(r.error ? `✗ ${folder}: ${r.error}`
    : auto ? `✓ ${folder}: 启动后自动发送 ${r.messages} 条 RPC 消息 / ${windowMs / 1000}s，${r.senders}/${r.nodeCount} 个节点 ${JSON.stringify(r.methods)}`
    : r.started ? `✓ ${folder}: 空闲时不发送；按 distvis:start 从 ${r.start.node} 调用 ${r.start.method} 后产生 ${r.started} 条 RPC 消息`
    : `✗ ${folder}: 空闲 ${windowMs / 1000}s 内只有 ${r.messages} 条 RPC 消息，且代码没有 // distvis:start 说明从哪里开始`);
  if (!r.error) assert.equal(r.commands, 0, `${folder}: idle window must not contain inputs`);
}

const failed = results.filter(r => r.error || (r.messages < minMessages && !r.started));
const automatic = results.filter(r => !r.error && r.messages >= minMessages).length;
console.log(`\n${automatic}/${results.length} 个协议启动后自动发送消息；${results.filter(r => r.started).length} 个按 distvis:start 开始；${failed.length} 个不合格`);
if (failed.length && !reportOnly) {
  console.error('不合格：' + failed.map(r => r.folder).join(', '));
  process.exitCode = 1;
}
