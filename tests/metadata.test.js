import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readdirSync, readFileSync, existsSync, mkdtempSync, mkdirSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';
import { tmpdir } from 'node:os';
import { fileURLToPath } from 'node:url';
import { protocolMetadata } from '../server/metadata.js';
import { ProtocolLibrary, readProtocolMetadata, snapshotWorkspace } from '../server/library.js';

const main = header => ({ path: 'main.go', content: `${header}\npackage main\nfunc main(){}\n` });
const start = header => protocolMetadata([main(header)]).start;

test('distvis:start names the node, input and arguments that begin message traffic', () => {
  assert.deepEqual(start('// distvis:start node-2 Application.Acquire {"holdMs":2000,"tag":{"s":"}"}} — node-2 申请锁'),
    { node: 'node-2', method: 'Application.Acquire', values: { holdMs: 2000, tag: { s: '}' } }, note: 'node-2 申请锁' });
  assert.deepEqual(start('/*\n * distvis:start node-1 /lesson.Application/Propose\n */'), { node: 'node-1', method: '/lesson.Application/Propose', values: {} });
  assert.equal(start('// distvis:name Demo'), undefined);
  for (const bad of ['', 'node-1', 'node-0 A.B', 'node-13 A.B', 'node-1 Sync', 'node-1 A.B {"x":', `node-1 A.B {} ${'x'.repeat(301)}`]) {
    assert.throws(() => start(`// distvis:start ${bad}`), /distvis:start/, bad);
  }
  assert.throws(() => start('// distvis:start node-1 A.B\n// distvis:start node-2 A.B'), /重复/);
  assert.throws(() => protocolMetadata([main('')], { start: { node: 'node-1', method: 'A.B', values: [1] } }), /distvis:start/);
});

test('library discovery and editor snapshots carry distvis:start to the run configuration', () => {
  const data = mkdtempSync(join(tmpdir(), 'distvis-start-')), dir = join(data, 'protocols', 'demo');
  mkdirSync(dir, { recursive: true });
  const code = '// distvis:start node-3 Application.Put {"key":"x"} 写入一个键\npackage main\nfunc main(){}\n';
  writeFileSync(join(dir, 'go.mod'), 'module demo\n\ngo 1.24\n');
  writeFileSync(join(dir, 'main.go'), code);
  const expected = { node: 'node-3', method: 'Application.Put', values: { key: 'x' }, note: '写入一个键' };
  assert.deepEqual(new ProtocolLibrary(data, join(data, 'protocols')).select('directory:demo').start, expected);
  assert.deepEqual(snapshotWorkspace({ files: [{ path: 'go.mod', content: 'module demo\n' }, { path: 'main.go', content: code }] }).start, expected);
});

// Protocols whose nodes stay silent until they get an application input. Each must say in code where
// traffic begins; tests/idle-traffic-smoke.mjs runs that input against real Go nodes.
const requestDriven = ['grpc-broadcast'];
// Protocols that send messages on their own after start (elections, heartbeats, periodic sync, anti-entropy, token).
const selfStarting = ['lww-store', 'netrpc-token', 'raft-election'];

test('every bundled protocol either starts traffic itself or names its first input with distvis:start', () => {
  const root = fileURLToPath(new URL('../protocols/', import.meta.url));
  const folders = readdirSync(root).filter(f => existsSync(join(root, f, 'go.mod'))).sort();
  assert.deepEqual(folders, [...requestDriven, ...selfStarting].sort(), 'classify every new protocol in this test');
  for (const folder of folders) {
    const meta = readProtocolMetadata(join(root, folder));
    if (selfStarting.includes(folder)) { assert.equal(meta.start, undefined, `${folder} starts by itself`); continue; }
    const { node, method, values } = meta.start || assert.fail(`${folder}: add // distvis:start to the Go file header`);
    assert.ok(meta.start.note, `${folder}: explain what the first input does`);
    const sources = readdirSync(join(root, folder), { recursive: true }).filter(f => f.endsWith('.go') || f.endsWith('.proto'))
      .map(f => readFileSync(join(root, folder, f), 'utf8')).join('\n');
    const [service, name] = method.startsWith('/') ? method.slice(1).split('/') : method.split('.');
    if (method.startsWith('/')) assert.match(sources, new RegExp(`service ${service.split('.').at(-1)}\\b[\\s\\S]*?rpc ${name}\\(`), `${folder}: ${method} is a gRPC method`);
    else {
      assert.match(sources, new RegExp(`RegisterName\\("${service}",[^\\n]*lab\\.Application\\(\\)`), `${folder}: ${service} is an application service`);
      assert.match(sources, new RegExp(`\\) ${name}\\(args `), `${folder}: ${method} exists`);
    }
    const index = Number(node.slice(5));
    const nodeCount = existsSync(join(root, folder, 'scenarios.json'))
      ? Math.min(...JSON.parse(readFileSync(join(root, folder, 'scenarios.json'), 'utf8')).scenarios.map(s => s.settings?.nodeCount ?? 5)) : 3;
    assert.ok(index <= nodeCount, `${folder}: ${node} exists in the smallest scenario (${nodeCount} nodes)`);
    assert.equal(typeof values, 'object');
  }
});
