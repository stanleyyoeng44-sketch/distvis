#!/usr/bin/env node
// Lecture scenarios: sync protocols/<folder> to the platform, create one experiment per
// scenario, and optionally replay a scenario's inputs/faults against real Go nodes.
//
//   node scripts/scenarios.mjs list
//   node scripts/scenarios.mjs setup [folder...]
//   node scripts/scenarios.mjs run <folder> [scenarioId...] [--keep]
//
// Scenario files live in protocols/<folder>/scenarios.json. See the scenario format appendix in docs/LAB1.md.
// DISTVIS_SCENARIO_DIR=<dir> reads <dir>/<folder>/scenarios.json instead (code still comes from
// protocols/<folder>), e.g. to grade extra scenarios without touching the protocol directory.
import { readFileSync, readdirSync, existsSync } from 'node:fs';
import { join, resolve, dirname } from 'node:path';
import { fileURLToPath } from 'node:url';

const base = process.env.DISTVIS_URL || 'http://localhost:3000';
const root = resolve(dirname(fileURLToPath(import.meta.url)), '..', 'protocols');
const scenarioRoot = process.env.DISTVIS_SCENARIO_DIR ? resolve(process.env.DISTVIS_SCENARIO_DIR) : root;
const sleep = ms => new Promise(r => setTimeout(r, ms));

async function api(path, body) {
  const res = await fetch(base + path, body === undefined ? {} : {
    method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body),
  });
  const data = await res.json().catch(() => ({}));
  if (!res.ok) { const err = new Error(`${res.status} ${path}: ${data.error || res.statusText}`); err.status = res.status; throw err; }
  return data;
}

function folders() {
  return readdirSync(root, { withFileTypes: true })
    .filter(d => d.isDirectory() && existsSync(join(scenarioRoot, d.name, 'scenarios.json')))
    .map(d => d.name).sort();
}
function load(folder) {
  const file = join(scenarioRoot, folder, 'scenarios.json');
  if (!existsSync(file)) throw new Error(`${folder} 没有 scenarios.json`);
  return JSON.parse(readFileSync(file, 'utf8'));
}

// ---- platform objects ------------------------------------------------------

async function ensureProtocol(folder) {
  const library = await api('/api/library');
  if (resolve(library.root) !== root) throw new Error(`平台协议根目录是 ${library.root}，需要 ${root}`);
  const entry = library.entries.find(e => e.folder === folder);
  if (!entry) throw new Error(`平台没有发现 ${folder}：${library.errors.join('; ')}`);
  const directory = join(root, folder);
  const list = await api('/api/protocol-projects');
  const found = list.find(p => !p.archived && p.origin?.directory === directory);
  const snapshot = await api(`/api/project?protocol=${encodeURIComponent(entry.id)}`);
  if (!found) {
    const created = await api('/api/protocol-projects', { name: entry.name, protocol: entry.id });
    console.log(`+ 协议 ${entry.name}`);
    return created;
  }
  const full = await api(`/api/protocol-projects/${found.id}`);
  if (full.projects?.shared?.sha256 === snapshot.sha256 && !Object.keys(full.projects).some(k => k !== 'shared')) return full;
  const updated = await api(`/api/protocol-projects/${found.id}`, {
    revision: full.revision, projects: { shared: { entry: snapshot.entry, encoding: 'base64', files: snapshot.files.map(f => ({ path: f.path, content: f.content, encoding: f.encoding || 'base64' })) } },
  });
  console.log(`~ 协议 ${entry.name} 已同步目录代码 (revision ${updated.revision})`);
  return updated;
}

function settingsOf(scenario) {
  const base = { runtime: 'docker', nodeCount: 5, latency: 80, bandwidth: 128, seed: 42, ...scenario.settings };
  if (process.env.DISTVIS_RUNTIME) base.runtime = process.env.DISTVIS_RUNTIME;
  return base;
}
async function ensureExperiment(protocol, scenario) {
  const list = await api(`/api/protocol-projects/${protocol.id}/experiments`);
  const settings = settingsOf(scenario);
  const found = list.find(e => e.name === scenario.name && !e.archived);
  if (!found) {
    const created = await api('/api/experiments', { name: scenario.name, protocolId: protocol.id, settings });
    console.log(`  + 实验 ${scenario.name}`);
    return created;
  }
  const same = Object.entries(settings).every(([k, v]) => found.settings?.[k] === v);
  if (same) return found;
  const full = await api(`/api/experiments/${found.id}`);
  return api(`/api/experiments/${found.id}`, { revision: full.revision, settings });
}

async function setup(folder) {
  const spec = load(folder);
  const protocol = await ensureProtocol(folder);
  const experiments = [];
  for (const scenario of spec.scenarios) experiments.push(await ensureExperiment(protocol, scenario));
  console.log(`✓ ${folder}: ${base}/#protocol/${protocol.id}/experiments`);
  return { spec, protocol, experiments };
}

// ---- conditions ------------------------------------------------------------

function get(obj, path) {
  if (!path) return obj;
  return path.split('.').reduce((v, k) => (v == null ? undefined : v[k]), obj);
}
function compare(actual, op, expected) {
  switch (op ?? '==') {
    case '==': return JSON.stringify(actual) === JSON.stringify(expected);
    case '!=': return JSON.stringify(actual) !== JSON.stringify(expected);
    case '<': return actual < expected;
    case '<=': return actual <= expected;
    case '>': return actual > expected;
    case '>=': return actual >= expected;
    case 'abs<=': return Math.abs(actual) <= expected;
    case 'abs>': return Math.abs(actual) > expected;
    case 'includes': return Array.isArray(actual) ? actual.some(x => JSON.stringify(x) === JSON.stringify(expected)) : String(actual ?? '').includes(expected);
    case 'exists': return actual !== undefined && actual !== null;
    case 'missing': return actual === undefined || actual === null;
    case 'in': return expected.some(x => JSON.stringify(x) === JSON.stringify(actual));
    default: throw new Error(`未知比较 ${op}`);
  }
}

class View {
  constructor(run) { this.run = run; this.events = run.events; this.states = {}; this.online = {}; this.schemas = {};
    for (const e of this.events) {
      if (e.type === 'state') this.states[e.node] = e.state;
      if (e.type === 'node') this.online[e.node] = e.online;
      if (e.type === 'input_schema') this.schemas[e.node] = e.schema;
      if (e.type === 'lifecycle' && e.action === 'start' && Array.isArray(e.nodes)) this.all = e.nodes.map(n => n.id || n);
    }
  }
  nodes(sel) {
    const all = this.all || Object.keys(this.states).sort();
    if (sel === '*' || sel === 'any') return all.filter(n => this.online[n] !== false);
    return Array.isArray(sel) ? sel : [sel];
  }
  describe(cond) {
    if (cond.node) return this.nodes(cond.node).map(n => `${n}.${cond.path}=${JSON.stringify(get(this.states[n], cond.path))}`).join(', ');
    if (cond.same) return this.nodes(cond.nodes || '*').map(n => `${n}:${JSON.stringify(get(this.states[n], cond.same))}`).join(' ');
    if (cond.messages) return `count=${this.messageCount(cond.messages)}`;
    return '';
  }
  messageCount(m) {
    return this.events.filter(e => e.type === 'send' && e.payload?.type === 'RPCRequest' && (!m.method || e.payload.method === m.method)).length;
  }
  test(cond) {
    if (cond.all) return cond.all.every(c => this.test(c));
    if (cond.any) return cond.any.some(c => this.test(c));
    if (cond.not) return !this.test(cond.not);
    if (cond.node) {
      const nodes = this.nodes(cond.node);
      const check = n => compare(get(this.states[n], cond.path), cond.op, cond.value);
      return cond.node === 'any' ? nodes.some(check) : nodes.length > 0 && nodes.every(check);
    }
    if (cond.same) {
      const values = this.nodes(cond.nodes || '*').map(n => JSON.stringify(get(this.states[n], cond.same)));
      return values.length > 0 && values.every(v => v === values[0] && v !== undefined);
    }
    if (cond.differ) {
      const values = new Set(this.nodes(cond.nodes || '*').map(n => JSON.stringify(get(this.states[n], cond.differ))));
      return values.size > 1;
    }
    if (cond.messages) return compare(this.messageCount(cond.messages), cond.messages.op || '>=', cond.messages.value);
    if (cond.atMostOne || cond.moreThanOne) {
      // History invariant: replay state events and count nodes matching at every point.
      const spec = cond.atMostOne || cond.moreThanOne, current = {};
      let max = 0;
      for (const e of this.events) {
        if (e.type === 'state') current[e.node] = compare(get(e.state, spec.path), spec.op, spec.value);
        if (e.type === 'node' && e.online === false) current[e.node] = false;
        max = Math.max(max, Object.values(current).filter(Boolean).length);
      }
      return cond.atMostOne ? max <= 1 : max > 1;
    }
    throw new Error(`无法识别的条件 ${JSON.stringify(cond)}`);
  }
}

// ---- running ---------------------------------------------------------------

async function waitIdle() {
  for (let i = 0; ; i++) {
    const active = (await api('/api/runs')).filter(r => ['starting', 'running'].includes(r.status));
    if (!active.length) return;
    if (i % 20 === 0) console.log(`  … 等待其他运行结束 (${active.map(r => r.config?.name || r.id).join(', ')})`);
    if (i > 900) throw new Error('等待空闲超时');
    await sleep(2000);
  }
}
async function startRun(experiment, name) {
  for (;;) {
    await waitIdle();
    try { return await api('/api/runs', { experimentId: experiment.id, name }); }
    catch (e) { if (e.status !== 409) throw e; await sleep(3000); }
  }
}

async function runScenario(folder, protocol, experiment, scenario, keep) {
  console.log(`\n▶ ${scenario.name} (${scenario.id})`);
  const run = await startRun(experiment, `自动场景 · ${scenario.id}`);
  const path = `/api/runs/${run.id}`;
  const view = async () => new View(await api(path));
  const results = [];
  let failed = false;
  try {
    const n = settingsOf(scenario).nodeCount;
    const ready = Date.now() + 240000;
    for (;;) {
      const v = await view();
      if (['failed', 'completed', 'interrupted'].includes(v.run.status)) {
        const err = v.events.filter(e => e.level === 'error').map(e => e.message).join('\n');
        throw new Error(`运行 ${v.run.status}: ${err.slice(0, 3000)}`);
      }
      if (v.run.status === 'running' && Object.keys(v.schemas).length >= n) break;
      if (Date.now() > ready) throw new Error('等待节点 input_schema 超时');
      await sleep(700);
    }
    const t0 = Date.now();
    const stamp = () => `${((Date.now() - t0) / 1000).toFixed(1).padStart(5)}s`;
    const pending = [];
    for (const step of scenario.steps) {
      if (step.note) console.log(`  ${stamp()} · ${step.note}`);
      if (step.wait) await sleep(step.wait);
      if (step.fault) {
        const f = { ...step.fault };
        if (f.kind === 'link') Object.assign(f, { latency: settingsOf(scenario).latency, bandwidth: settingsOf(scenario).bandwidth, blocked: false, bidirectional: false, ...step.fault });
        for (let i = 0; ; i++) {
          try { await api(path + '/faults', f); break; } catch (e) { if (e.status !== 409 || i > 20) throw e; await sleep(300); }
        }
        console.log(`  ${stamp()} ⚡ ${JSON.stringify(step.fault)}`);
      }
      if (step.input || step.concurrent) {
        // `concurrent` delivers every input at the same coordinator instant (batch API).
        const items = step.concurrent || [step.input];
        let v = await view();
        const find = ({ node, method }) => (v.schemas[node] || []).find(s => s.label === method);
        for (let i = 0; items.some(it => !find(it)) && i < 60; i++) { await sleep(500); v = await view(); }
        for (const it of items) if (!find(it)) throw new Error(`${it.node} 没有声明 ${it.method}`);
        let ids;
        if (step.concurrent) {
          const r = await api(path + '/commands', { batch: items.map(it => ({ node: it.node, action: find(it).action, values: it.values || {} })) });
          const failedItems = r.commands.filter(c => c.error);
          if (failedItems.length) throw new Error(`并发输入失败: ${failedItems.map(c => `${c.node} ${c.error}`).join('; ')}`);
          ids = r.commands.map(c => c.id);
          console.log(`  ${stamp()} ⇉ 同时: ${items.map(it => `${it.node} ${it.method} ${JSON.stringify(it.values || {})}`).join(' | ')}`);
        } else {
          const { node, method, values = {} } = step.input;
          ids = [(await api(path + '/commands', { node, action: find(step.input).action, values })).id];
          console.log(`  ${stamp()} → ${node} ${method} ${JSON.stringify(values)}`);
        }
        const waitOne = async ({ node, method }, id) => {
          const end = Date.now() + (step.timeout || 20000);
          while (Date.now() < end) {
            const r = (await view()).events.find(e => e.type === 'command_result' && e.commandId === id);
            if (r) {
              console.log(`  ${stamp()} ← ${node} ${method}: ${r.error ? '错误 ' + r.error : JSON.stringify(r.result)}`);
              if (step.expect === 'ok' && r.error) throw new Error(`${method} 预期成功，实际 ${r.error}`);
              if (step.expect === 'error' && !r.error) throw new Error(`${method} 预期失败，实际成功`);
              return r;
            }
            await sleep(400);
          }
          if (step.expect === 'timeout') { console.log(`  ${stamp()} ← ${node} ${method}: 超时（符合预期）`); return null; }
          throw new Error(`${method} 在 ${step.timeout || 20000}ms 内没有返回`);
        };
        const wait = Promise.all(items.map((it, i) => waitOne(it, ids[i])));
        if (step.await === false) pending.push(wait.catch(e => { console.log(`  ✗ ${e.message}`); failed = true; }));
        else await wait;
      }
      if (step.until) {
        const end = Date.now() + (step.timeout || 20000);
        let v = await view();
        while (!v.test(step.until) && Date.now() < end) { await sleep(500); v = await view(); }
        const ok = v.test(step.until);
        console.log(`  ${stamp()} ${ok ? '✓' : '✗'} 等到: ${step.label || JSON.stringify(step.until)} ${ok ? '' : '→ ' + v.describe(step.until)}`);
        results.push({ label: step.label || JSON.stringify(step.until), ok });
        if (!ok) failed = true;
      }
      if (step.check) {
        const v = await view();
        const ok = v.test(step.check);
        console.log(`  ${stamp()} ${ok ? '✓' : '✗'} ${step.label || JSON.stringify(step.check)} ${ok ? '' : '→ ' + v.describe(step.check)}`);
        results.push({ label: step.label || JSON.stringify(step.check), ok });
        if (!ok) failed = true;
      }
    }
    await Promise.all(pending);
  } catch (e) {
    failed = true;
    console.log(`  ✗ ${e.message}`);
  } finally {
    const current = await api(path).catch(() => null);
    if (current?.status === 'running' && !keep) await api(path + '/stop', {}).catch(() => {});
    if (current?.status === 'starting') console.log('  ! 运行仍在 starting，稍后请在平台结束');
  }
  console.log(`  ${failed ? '✗ 失败' : '✓ 通过'} · 回放: ${base}/#experiment/${experiment.id}/history · run ${run.id}`);
  return !failed;
}

const [cmd, ...rest] = process.argv.slice(2);
const keep = rest.includes('--keep');
const args = rest.filter(a => a !== '--keep');
try {
  if (cmd === 'list') {
    for (const folder of folders()) {
      const spec = load(folder);
      console.log(`${folder}`);
      for (const s of spec.scenarios) console.log(`  ${s.id.padEnd(22)} ${s.name}`);
    }
  } else if (cmd === 'setup') {
    for (const folder of args.length ? args : folders()) await setup(folder);
  } else if (cmd === 'run') {
    const [folder, ...ids] = args;
    if (!folder) throw new Error('用法: run <folder> [scenarioId...] [--keep]');
    const { spec, protocol, experiments } = await setup(folder);
    let ok = true;
    for (const [i, scenario] of spec.scenarios.entries()) {
      if (ids.length && !ids.includes(scenario.id)) continue;
      ok = (await runScenario(folder, protocol, experiments[i], scenario, keep)) && ok;
    }
    process.exitCode = ok ? 0 : 1;
  } else {
    console.log('用法: node scripts/scenarios.mjs list | setup [folder...] | run <folder> [scenarioId...] [--keep]');
  }
} catch (e) {
  console.error(e.message);
  process.exitCode = 1;
}
