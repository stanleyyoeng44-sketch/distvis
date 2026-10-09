import { test } from 'node:test';
import assert from 'node:assert/strict';
import { mkdtempSync, readFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Readable, Writable } from 'node:stream';

// Exercise the real HTTP handler without opening a port, so tests also run in restricted sandboxes.
process.env.DISTVIS_DATA = mkdtempSync(join(tmpdir(), 'distvis-api-'));
const { server } = await import('../server/index.js');
function request(method, url, data, headers = {}) {
  return new Promise((resolve, reject) => {
    const req = Readable.from(data === undefined ? [] : [JSON.stringify(data)]);
    Object.assign(req, { method, url, headers: { host: 'localhost:3000', ...headers } });
    let text = '';
    const res = new Writable({ write(chunk, encoding, done) { text += chunk; done(); } });
    res.headers = {};
    res.setHeader = (key, value) => { res.headers[key] = value; };
    res.writeHead = (status, headers) => { res.statusCode = status; Object.assign(res.headers, headers); res.headersSent = true; };
    res.on('finish', () => {
      let body = text; try { body = JSON.parse(text); } catch {}
      resolve({ status: res.statusCode, headers: res.headers, body });
    });
    res.on('error', reject);
    server.emit('request', req, res);
  });
}
test('complete API lifecycle, fault archive, state replay data, export and restart persistence', async () => {
  const created = await request('POST', '/api/runs', { protocol: 'gossip', runtime: 'simulation', nodeCount: 3, name: 'Persistence test' });
  assert.equal(created.status, 201);
  const id = created.body.id;
  await new Promise(resolve => setTimeout(resolve, 150));
  const duplicate = await request('POST', '/api/runs', { protocol: 'raft' });
  assert.equal(duplicate.status, 409);
  const crash = await request('POST', `/api/runs/${id}/faults`, { kind: 'crash', node: 'node-2' });
  assert.equal(crash.status, 200);
  assert.equal((await request('POST', `/api/runs/${id}/commands`, { node: 'node-2', action: 'write', values: { key: 'x', value: 'y' } })).status, 400);
  assert.equal((await request('POST', `/api/runs/${id}/commands`, { node: 'node-1', action: 'unknown', values: {} })).status, 400);
  assert.equal((await request('POST', `/api/runs/${id}/commands`, { node: 'node-1', action: 'write', values: null })).status, 400);
  const input = await request('POST', `/api/runs/${id}/commands`, { node: 'node-1', action: 'write', values: { key: 'application', value: 'typed input' } });
  assert.equal(input.status, 200);
  assert.equal(input.body.id, `input-${input.body.commandSeq}`);
  assert.equal((await request('POST', `/api/runs/${id}/commands`, { node: 'node-1', key: 'answer', value: '42' })).status, 200);
  const detail = await request('GET', `/api/runs/${id}`);
  assert.ok(detail.body.events.some(e => e.type === 'fault' && e.fault.node === 'node-2'));
  assert.ok(detail.body.events.some(e => e.type === 'state' && e.state.store?.answer?.value === '42'));
  assert.ok(detail.body.events.some(e => e.type === 'state' && e.state.store?.application?.value === 'typed input'));
  assert.equal(detail.body.events.filter(e => e.type === 'input_schema').length, 3);
  const stopped = await request('POST', `/api/runs/${id}/stop`, {});
  assert.equal(stopped.body.status, 'completed');
  assert.equal((await request('POST', `/api/runs/${id}/commands`, { node: 'node-1', key: 'x', value: 'y' })).status, 400);
  const exp = await request('GET', `/api/runs/${id}/export`);
  assert.equal(exp.body.schemaVersion, 1);
  assert.match(Buffer.from(exp.body.project.files.find(f=>f.path==='main.go').content,'base64').toString(),/lab.Main/);
  assert.equal(exp.body.events.at(-1).action, 'stop');
  const onDisk = readFileSync(join(process.env.DISTVIS_DATA, id, 'events.jsonl'), 'utf8').trim().split('\n').map(JSON.parse);
  assert.deepEqual(exp.body.events, onDisk);
  const { server: reloaded } = await import(`../server/index.js?reload=${Date.now()}`);
  // Query reloaded module's handler to verify disk reconstruction.
  const oldEmit = server.emit;
  server.emit = (...args) => reloaded.emit(...args);
  try {
    const restored = await request('GET', `/api/runs/${id}`);
    assert.equal(restored.body.status, 'completed');
    assert.deepEqual(restored.body.events, onDisk);
  } finally { server.emit = oldEmit; }
});
test('rejects invalid configuration, cross-origin writes and path traversal', async () => {
  assert.equal((await request('POST', '/api/runs', { nodeCount: -1 })).status, 400);
  assert.equal((await request('POST', '/api/runs', {}, { origin: 'https://example.com' })).status, 403);
  assert.equal((await request('GET', '/../../etc/passwd')).status, 404);
  assert.equal((await request('GET', '/api/runs/not-a-uuid')).status, 404);
});
test('serves the application and SDK example without external assets', async () => {
  const page = await request('GET', '/');
  assert.equal(page.status, 200);
  assert.ok(page.body.includes('网络拓扑'));
  assert.ok(page.headers['Content-Security-Policy']);
  assert.equal((await request('GET', '/app.js')).status, 200);
  assert.equal((await request('GET', '/api/example')).status, 200);
});

test('serves API documentation and exact downloadable example through fixed routes', async () => {
  const home = await request('GET', '/');
  assert.match(home.body, /API 文档/);
  assert.ok(!home.body.includes('guide-dialog'));
  for (const [key, topic] of [['go', 'Runtime 生命周期'], ['platform', 'POST /api/runs'], ['agent', 'distvis_scaffold_protocol']]) {
    const page = await request('GET', `/docs/${key}`);
    assert.equal(page.status, 200);
    assert.match(page.headers['Content-Type'], /text\/html/);
    assert.match(page.headers['Content-Security-Policy'], /script-src 'self'/);
    assert.ok(page.body.includes(topic));
    assert.ok(page.body.includes('copy-code'));
    assert.ok(page.body.includes('docs-search'));
    assert.ok(!/<(?:script|link)[^>]+(?:src|href)="https?:/.test(page.body));
    assert.match((await request('GET', `/docs/${key}.md`)).body, /^# /);
  }
  assert.equal((await request('GET', '/docs')).status, 200);
  assert.match((await request('GET', '/docs/distvis-protocol/SKILL.md')).body, /name: distvis-protocol/);
  assert.ok(!home.body.includes('teaching-examples'));
  for (const path of ['/docs.js', '/docs.css', '/docs/examples/ping/main.go', '/docs/examples/ping/go.mod']) {
    assert.equal((await request('GET', path)).status, 200);
  }
  const goDoc = (await request('GET', '/docs/go.md')).body;
  const example = (await request('GET', '/docs/examples/ping/main.go')).body;
  assert.equal(goDoc.match(/```go\n([\s\S]+?)\n```/)[1] + '\n', example);
  for (const path of ['/docs/no-such-page', '/docs/examples/ping/secret', '/docs/%2e%2e%2fpackage.json']) {
    assert.equal((await request('GET', path)).status, 404);
  }
  assert.equal((await request('POST', '/docs/go', {})).status, 404);
});

test('project templates and validation expose runnable multi-file projects without writing user files', async () => {
  const examples=await request('GET','/api/examples');
  assert.deepEqual(examples.body.map(p=>p.id),['raft','token','gossip','grpc']);
  for(const example of examples.body){
    const {body:project,status}=await request('GET',`/api/project?protocol=${example.id}`);
    assert.equal(status,200);
    assert.ok(project.files.some(f=>f.path==='protocol.go'));
    assert.match(Buffer.from(project.files.find(f=>f.path==='main.go').content,'base64').toString(),/lab.Main/);
    const workspace={...project,files:project.files.map(f=>({...f,encoding:'base64'}))};
    const valid=await request('POST','/api/workspace/validate',{workspace});
    assert.equal(valid.status,200);
    assert.equal(valid.body.sha256,project.sha256);
    assert.equal((await request('POST','/api/workspace/validate',{workspace,nodeWorkspaces:{'node-99':workspace}})).status,400);
  }
  assert.equal((await request('GET','/api/project?protocol=../../etc')).status,400);
  assert.equal((await request('POST','/api/runs',{protocol:'custom',workspace:{files:[{path:'../escape',content:'bad'}]}})).status,400);
});

test('protocol code is shared by child experiments, while settings, histories and snapshots stay isolated', async () => {
  const parent=(await request('POST','/api/protocol-projects',{name:'Shared Raft',protocol:'raft'})).body;
  const other=(await request('POST','/api/protocol-projects',{name:'Other Raft',protocol:'raft'})).body;
  const a=(await request('POST','/api/experiments',{name:'Normal',protocolId:parent.id,settings:{runtime:'simulation',nodeCount:3,latency:80}})).body;
  const b=(await request('POST','/api/experiments',{name:'Slow network',protocolId:parent.id,settings:{runtime:'simulation',nodeCount:5,latency:800}})).body;
  assert.equal(a.protocolId,b.protocolId);assert.equal(a.projects,undefined);
  assert.equal((await request('GET',`/api/protocol-projects/${parent.id}/experiments`)).body.length,2);
  assert.deepEqual((await request('GET',`/api/protocol-projects/${other.id}/experiments`)).body,[]);
  const created=await request('POST','/api/runs',{experimentId:a.id});assert.equal(created.status,201);
  const first=created.body;
  assert.equal(first.config.nodeCount,3);assert.equal(first.config.protocolId,parent.id);assert.equal(first.config.protocolRevision,1);
  await request('POST',`/api/runs/${first.id}/stop`,{});
  const archive=(await request('GET',`/api/runs/${first.id}/source`)).body.project;
  const projects=structuredClone(parent.projects);
  projects.shared.files=projects.shared.files.map(file=>({...file,encoding:'base64'}));
  const main=projects.shared.files.find(file=>file.path==='main.go');
  main.content=Buffer.from(Buffer.from(main.content,'base64').toString()+'\n// protocol version two\n').toString('base64');
  const saved=await request('POST',`/api/protocol-projects/${parent.id}`,{revision:1,projects});assert.equal(saved.status,200);
  assert.equal((await request('POST',`/api/protocol-projects/${parent.id}`,{revision:1,projects})).status,400);
  assert.equal((await request('POST',`/api/experiments/${a.id}`,{revision:1,projects})).status,400,'child cannot own code');
  const changed=await request('POST',`/api/experiments/${a.id}`,{revision:1,settings:{runtime:'simulation',nodeCount:4,latency:100}});
  assert.equal(changed.status,200);
  assert.equal((await request('GET',`/api/experiments/${b.id}`)).body.settings.latency,800);
  assert.equal((await request('GET',`/api/protocol-projects/${other.id}`)).body.projects.shared.sha256,other.projects.shared.sha256);
  for(const child of [a,b]){
    const next=(await request('POST','/api/runs',{experimentId:child.id})).body;
    assert.equal(next.config.protocolRevision,2);
    if(child===b){assert.equal(next.config.nodeCount,5);assert.equal(next.config.latency,800);}
    await request('POST',`/api/runs/${next.id}/stop`,{});
    assert.equal((await request('GET',`/api/runs/${next.id}/source`)).body.project.sha256,saved.body.projects.shared.sha256);
  }
  assert.equal((await request('GET',`/api/runs/${first.id}/source`)).body.project.sha256,archive.sha256);
  assert.equal((await request('GET',`/api/experiments/${a.id}/runs`)).body.length,2);
  assert.equal((await request('GET',`/api/experiments/${b.id}/runs`)).body.length,1);
  const summary=(await request('GET','/api/protocol-projects')).body.find(p=>p.id===parent.id);
  assert.equal(summary.experimentCount,2);assert.equal(summary.runCount,3);assert.equal(summary.projects,undefined);
  const {ExperimentStore}=await import('../server/experiments.js');const store=new ExperimentStore(process.env.DISTVIS_DATA);
  assert.equal(store.get(a.id).protocolId,parent.id);assert.equal(store.getProtocol(parent.id).revision,2);
});

test('acceptance requests archive their own workspaces without joining normal protocol histories',async()=>{
  const headers={'x-distvis-purpose':'acceptance'};
  const normal=(await request('POST','/api/protocol-projects',{name:'Student token',protocol:'token'})).body;
  const parent=(await request('POST','/api/protocol-projects',{name:'Acceptance token',protocol:'token'},headers)).body;
  assert.equal(parent.archived,true);assert.equal(normal.archived,false);
  const child=(await request('POST','/api/experiments',{name:'acceptance child',protocolId:parent.id})).body;
  assert.equal(child.archived,true);
  const result=await request('POST','/api/runs',{protocol:'token',runtime:'simulation',name:'Acceptance run'},headers);
  assert.equal(result.status,201);
  const run=result.body;
  try{
    const workspace=(await request('GET',`/api/experiments/${run.config.experimentId}`)).body;
    const owner=(await request('GET',`/api/protocol-projects/${workspace.protocolId}`)).body;
    assert.equal(workspace.archived,true);assert.equal(owner.archived,true);
    assert.notEqual(owner.id,normal.id);
    const restored=await request('POST',`/api/protocol-projects/${parent.id}`,{revision:parent.revision,archived:false});
    assert.equal(restored.status,200);assert.equal(restored.body.archived,false);
    assert.equal((await request('POST',`/api/protocol-projects/${parent.id}`,{revision:parent.revision,archived:true})).status,400);
  }finally{await request('POST',`/api/runs/${run.id}/stop`,{});}
});
