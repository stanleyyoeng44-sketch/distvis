import http from 'node:http';
import { readFileSync, writeFileSync, appendFileSync, mkdirSync, readdirSync, existsSync } from 'node:fs';
import { join, extname } from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';
import { Experiment, protocols, validateConfig } from './engine.js';
import { bufferedEventLog } from './event-log.js';
import { ContainerRuntime } from './runtime.js';
import { ProtocolLibrary, snapshotProject, snapshotWorkspace, readProtocolMetadata } from './library.js';
import { ExperimentStore, archiveDraft } from './experiments.js';
import { documentation } from './docs.js';

const root = fileURLToPath(new URL('../', import.meta.url));
const dataDir = process.env.DISTVIS_DATA || join(root, 'data');
mkdirSync(dataDir, { recursive: true });
const library = new ProtocolLibrary(dataDir, join(root, 'protocols'));
const examples = Object.entries({raft:'raft-election', token:'netrpc-token', gossip:'lww-store', grpc:'grpc-broadcast'}).map(([id,folder]) => {
  const directory = join(root,'protocols',folder);
  const manifest = readProtocolMetadata(directory);
  return {id,folder,directory,root:join(root,'protocols'),entry:'.',source:'example',...manifest};
});
const exampleProject = id => examples.find(p=>p.id===id);
const workspaces = new ExperimentStore(dataDir);
const runs = new Map();
const sourceDefault = () => readFileSync(join(root, 'examples/node/main.go'), 'utf8');
function persist(run) {
  writeFileSync(join(dataDir, run.id, 'meta.json'), JSON.stringify(run.info(), null, 2));
}
for (const entry of readdirSync(dataDir, { withFileTypes: true })) {
  if (!entry.isDirectory() || ['experiments','protocols'].includes(entry.name)) continue;
  try {
    const path = join(dataDir, entry.name);
    const meta = JSON.parse(readFileSync(join(path, 'meta.json'), 'utf8'));
    const events = meta.eventCount>60000 ? [] : readFileSync(join(path, 'events.jsonl'), 'utf8').trim().split('\n').filter(Boolean).flatMap(line => {
      try { return [JSON.parse(line)]; } catch { return []; } // tolerate a torn final append
    });
    if (['running', 'starting', 'created'].includes(meta.status)) {
      meta.status = 'interrupted';
      writeFileSync(join(path, 'meta.json'), JSON.stringify(meta, null, 2));
    }
    const run = new Experiment(meta.config, { id: meta.id, createdAt: meta.createdAt });
    run.status = meta.status;
    run.time = events.at(-1)?.time || meta.time || 0;
    run.events = events;
    if (meta.eventCount>60000) run.archivedEventCount = meta.eventCount;
    run.testResult = meta.testResult;
    runs.set(run.id, run);
  } catch (e) { console.error(`无法读取历史 ${entry.name}: ${e.message}`); }
}
for(const run of [...runs.values()].sort((a,b)=>b.createdAt.localeCompare(a.createdAt))){
  try{
    const source=JSON.parse(readFileSync(join(dataDir,run.id,'source.json'),'utf8'));
    const template=exampleProject(run.config.protocol);
    if(workspaces.adopt(run,source,template?snapshotProject(template):null))persist(run);
    const parent=workspaces.get(run.config.experimentId).protocolId;
    if(!run.config.protocolId){run.config.protocolId=parent;persist(run);}
  }catch(error){console.error(`无法迁移实验工作区 ${run.id}: ${error.message}`);}
}
const runtimes = new Map();
const launches = new Map();
let busy = false;
let shuttingDown = false;
function runEvents(run) {
  if (!run.archivedEventCount) return run.events;
  return readFileSync(join(dataDir, run.id, 'events.jsonl'), 'utf8').split('\n').filter(Boolean).flatMap(line=>{try{return [JSON.parse(line)];}catch{return [];}});
}
const timer = setInterval(() => {
  const now = performance.now();
  for (const run of runs.values()) {
    if (run.status === 'running') {
      const elapsed = run.lastTick ? now - run.lastTick : 2;
      // Virtual simulation time is deterministic; Go processes run on elapsed real time.
      run.advance(run.config.runtime === 'simulation' ? 2 : elapsed);
      run.lastTick = now;
    }
  }
}, 2);
timer.unref();

async function body(req) {
  let text = '';
  for await (const chunk of req) {
    text += chunk;
    if (Buffer.byteLength(text) > 48 * 1024 * 1024) throw new Error('请求超过 48 MiB');
  }
  return text ? JSON.parse(text) : {};
}
function json(res, status, data) {
  res.writeHead(status, { 'Content-Type': 'application/json; charset=utf-8', 'Cache-Control': 'no-store' });
  res.end(JSON.stringify(data));
}
async function launch(run, source, nodeSources, project) {
  try {
    if (run.config.runtime === 'simulation') run.start();
    else {
      const runtime = new ContainerRuntime(run, join(dataDir, run.id), source, nodeSources, project);
      runtimes.set(run.id, runtime);
      await runtime.start();
    }
  } catch (error) {
    run.status = 'failed';
    run.log('runtime', { level: 'error', phase: 'startup', message: error.message });
    // Clients watching the stream learn about the failure immediately instead of on the next poll.
    run.log('lifecycle', { action: 'failed', reason: '实验启动失败' });
    for (const node of run.nodes) {
      run.online[node] = false;
      run.log('node', { node, online: false, reason: '实验启动失败' });
    }
    await runtimes.get(run.id)?.cleanup();
  } finally { persist(run); }
}

export const server = http.createServer(async (req, res) => {
  try {
    if (shuttingDown) return json(res, 503, { error: 'coordinator 正在关闭' });
    const url = new URL(req.url, 'http://localhost');
    const acceptance=req.headers['x-distvis-purpose']==='acceptance';
    // Browser writes must originate from this local coordinator.
    if (req.method !== 'GET' && req.headers.origin && req.headers.origin !== `http://${req.headers.host}`) return json(res, 403, { error: '跨站写入已拒绝' });
    if (url.pathname === '/api/protocols') return json(res, 200, { ...protocols, ...Object.fromEntries(library.discover().entries.map(p=>[p.id,p])) });
    if (url.pathname === '/api/library' && req.method === 'GET') return json(res, 200, library.discover());
    if (url.pathname === '/api/library' && req.method === 'POST') {
      const input = await body(req);
      return json(res, 200, library.setRoot(input.root));
    }
    if (url.pathname === '/api/example') return json(res, 200, { source: sourceDefault() });
    if (url.pathname === '/api/examples' && req.method === 'GET') return json(res, 200, examples);
    if(url.pathname==='/api/protocol-projects' && req.method==='GET')return json(res,200,workspaces.listProtocols(runs));
    if(url.pathname==='/api/protocol-projects' && req.method==='POST'){
      const input=await body(req);
      if(acceptance)input.archived=true;
      if(!input.projects){
        const template=exampleProject(input.protocol) || library.select(input.protocol);
        input.projects={shared:snapshotProject(template)};
        input.origin={protocol:template.id,directory:template.directory,root:template.root || library.root};
      }else delete input.origin;
      return json(res,201,workspaces.createProtocol(input));
    }
    const protocolMatch=url.pathname.match(/^\/api\/protocol-projects\/([a-f0-9-]+)(?:\/(experiments))?$/);
    if(protocolMatch){
      const p=workspaces.getProtocol(protocolMatch[1]);
      if(protocolMatch[2] && req.method==='GET')return json(res,200,workspaces.list(runs).filter(e=>e.protocolId===p.id));
      if(!protocolMatch[2] && req.method==='GET')return json(res,200,p);
      if(!protocolMatch[2] && req.method==='POST')return json(res,200,workspaces.saveProtocol(p.id,await body(req)));
    }
    if (url.pathname === '/api/experiments' && req.method === 'GET') return json(res,200,workspaces.list(runs));
    if (url.pathname === '/api/experiments' && req.method === 'POST') {
      const input=await body(req);
      if(acceptance)input.archived=true;
      let projects=input.projects,origin;
      if(!projects && !input.protocolId){
        const template=exampleProject(input.protocol) || library.select(input.protocol);
        projects={shared:snapshotProject(template)};
        origin={protocol:template.id,directory:template.directory,root:template.root || library.root};
      }
      return json(res,201,workspaces.create({...input,projects,origin}));
    }
    const workspaceMatch=url.pathname.match(/^\/api\/experiments\/([a-f0-9-]+)(?:\/(runs))?$/);
    if(workspaceMatch){
      const item=workspaces.get(workspaceMatch[1]);
      if(workspaceMatch[2]==='runs' && req.method==='GET')return json(res,200,[...runs.values()].filter(r=>r.config.experimentId===item.id).map(r=>r.info()).sort((a,b)=>b.createdAt.localeCompare(a.createdAt)));
      if(!workspaceMatch[2] && req.method==='GET')return json(res,200,item);
      if(!workspaceMatch[2] && req.method==='POST')return json(res,200,workspaces.save(item.id,await body(req)));
    }
    if (url.pathname === '/api/project' && req.method === 'GET') {
      const id = url.searchParams.get('protocol');
      const project = exampleProject(id) || library.select(id);
      return json(res, 200, snapshotProject(project));
    }
    if (url.pathname === '/api/workspace/validate' && req.method === 'POST') {
      const input = await body(req);
      const project = snapshotWorkspace(input.workspace);
      if (input.nodeWorkspaces && (typeof input.nodeWorkspaces !== 'object' || Array.isArray(input.nodeWorkspaces))) throw new Error('节点项目应为对象');
      for (const [node,draft] of Object.entries(input.nodeWorkspaces || {})) {
        if (!/^node-([1-9]|1[0-2])$/.test(node)) throw new Error('节点项目名称不合法');
        snapshotWorkspace(draft);
      }
      return json(res,200,{ok:true,sha256:project.sha256,fileCount:project.files.length});
    }
    if (url.pathname === '/api/runs' && req.method === 'GET') return json(res, 200, [...runs.values()].map(r => r.info()).reverse());
    if (url.pathname === '/api/runs' && req.method === 'POST') {
      if (busy || [...runs.values()].some(r => ['running', 'starting'].includes(r.status))) return json(res, 409, { error: '请先结束当前实验' });
      busy = true;
      try {
        let input = await body(req);
        let savedWorkspace=null;
        let savedProtocol=null;
        if(input.experimentId){
          savedWorkspace=workspaces.get(input.experimentId);
          savedProtocol=workspaces.getProtocol(savedWorkspace.protocolId);
          input={...savedWorkspace.settings,...input};
          input.name ||= savedWorkspace.name;
          input.protocolId=savedProtocol.id;
          input.protocolRevision=savedProtocol.revision;
          input.protocol=['raft','token','gossip'].includes(savedWorkspace.protocol)?savedWorkspace.protocol:'custom';
          if(input.runtime==='simulation' && !['raft','token','gossip'].includes(input.protocol))throw new Error('此实验没有内置参考模型，请选择 Go 运行环境');
          input.workspace=archiveDraft(savedProtocol.projects.shared);
          input.nodeWorkspaces=Object.fromEntries(Object.entries(savedProtocol.projects).filter(([node])=>node!=='shared' && Number(node.slice(5))<=Number(input.nodeCount)).map(([node,p])=>[node,archiveDraft(p)]));
          input.source=undefined;input.nodeSources={};
        }
        delete input.project; // Always resolve project paths from the configured library.
        const project = savedWorkspace ? null : typeof input.protocol === 'string' && input.protocol.startsWith('directory:') ? {...library.select(input.protocol), root:library.root}
          : exampleProject(input.protocol || 'raft');
        const workspace = (savedWorkspace || input.protocol === 'custom') && input.workspace ? snapshotWorkspace(input.workspace) : null;
        const nodeWorkspaces = {};
        if (input.nodeWorkspaces && (!workspace || typeof input.nodeWorkspaces !== 'object' || Array.isArray(input.nodeWorkspaces))) throw new Error('节点项目需要默认项目');
        for (const [node, draft] of Object.entries(input.nodeWorkspaces || {})) {
          if (!/^node-([1-9]|1[0-2])$/.test(node) || Number(node.slice(5)) > Number(input.nodeCount ?? 5)) throw new Error('节点项目名称不合法');
          nodeWorkspaces[node] = snapshotWorkspace(draft);
        }
        if (project || workspace) input.project = project || {name:workspace.name,entry:workspace.entry,source:'workspace',...(workspace.start?{start:workspace.start}:{})};
        const config = validateConfig(input);
        if (config.protocol === 'custom' && !workspace && !input.source?.trim()) throw new Error('请在协议编辑器中保存 Go 项目');
        const source = project || workspace ? '' : config.protocol === 'custom' ? input.source : sourceDefault();
        if (typeof source !== 'string' || source.length > 200000) throw new Error('源码长度不合法');
        const nodeSources = input.nodeSources || {};
        if (typeof nodeSources !== 'object' || Array.isArray(nodeSources)) throw new Error('节点源码应为对象');
        for (const [node, text] of Object.entries(nodeSources)) {
          if (!/^node-([1-9]|1[0-2])$/.test(node) || Number(node.slice(5)) > config.nodeCount || typeof text !== 'string' || text.length > 200000) throw new Error('节点源码不合法');
        }
        const run = new Experiment(config);
        const path = join(dataDir, run.id);
        mkdirSync(path, { recursive: true });
        writeFileSync(join(path, 'events.jsonl'), '');
        const archive = project ? snapshotProject(project, join(path, 'project')) : workspace ? snapshotWorkspace({...workspace,files:workspace.files.map(f=>({...f,encoding:'base64'}))},join(path,'project')) : undefined;
        const nodeProjects = {};
        for (const [node,draft] of Object.entries(nodeWorkspaces)) nodeProjects[node] = snapshotWorkspace({...draft,files:draft.files.map(f=>({...f,encoding:'base64'}))},join(path,'projects',node));
        if (archive) run.config.project.sha256 = archive.sha256;
        writeFileSync(join(path, 'source.json'), JSON.stringify({ source, nodeSources, ...(archive ? { project:archive, nodeProjects } : {}) }, null, 2));
        if(!savedWorkspace){
          if(acceptance){
            const sourceProject=text=>({entry:'.',files:[
              {path:'main.go',content:text},{path:'go.mod',content:'module lesson/acceptance\n\ngo 1.24\n\nrequire distvis v0.0.0\n'}
            ]});
            const projects={shared:archive || sourceProject(source),...Object.fromEntries(Object.entries(nodeSources).map(([node,text])=>[node,sourceProject(text)])),...nodeProjects};
            const child=workspaces.create({name:config.name,protocol:config.protocol,projects,settings:config,archived:true,origin:project?{root:project.root,directory:project.directory}:undefined});
            run.config.experimentId=child.id;
          }else workspaces.adopt(run,{source,nodeSources,project:archive,nodeProjects},exampleProject(config.protocol)?snapshotProject(exampleProject(config.protocol)):null);
          run.config.protocolId=workspaces.get(run.config.experimentId).protocolId;
        }
        const eventLog = config.limits?.eventCount>60000 ? bufferedEventLog(join(path, 'events.jsonl')) : null;
        run.record = eventLog ? event => eventLog.record(event) : event => appendFileSync(join(path, 'events.jsonl'), JSON.stringify(event) + '\n');
        run.on('stop', () => {
          eventLog?.close();
          persist(run);
          const runtime = runtimes.get(run.id);
          if (runtime) runtime.cleanup().then(()=>{
            if (run.events.length>60000) { run.archivedEventCount=(run.archivedEventCount || 0)+run.events.length;run.events=[];persist(run); }
          }).catch(e => console.error(e));
        });
        runs.set(run.id, run);
        run.status = 'starting';
        persist(run);
        json(res, 201, run.info());
        const runtimeProject = archive ? {
          ...(project || workspace), root:project?.root || ([savedWorkspace?.origin?.root,savedProtocol?.origin?.root].find(p=>p && existsSync(p)) || join(path,'project')), snapshot:join(path,'project'),
          nodes:Object.fromEntries(Object.entries(nodeProjects).map(([node,p])=>[node,{entry:p.entry,snapshot:join(path,'projects',node)}])),
        } : null;
        const task = launch(run, source, nodeSources, runtimeProject);
        launches.set(run.id, task);
        task.catch(error => console.error(error)).finally(() => launches.delete(run.id));
      } finally { busy = false; }
      return;
    }
    const match = url.pathname.match(/^\/api\/runs\/([a-f0-9-]+)(?:\/(events|faults|stop|commands|export|source|checks))?$/);
    if (match) {
      const run = runs.get(match[1]);
      if (!run) return json(res, 404, { error: '实验不存在' });
      const action = match[2];
      if (!action && req.method === 'GET') return json(res, 200, { ...run.info(), ...(url.searchParams.has('summary') ? {} : { events: runEvents(run).slice(Number(url.searchParams.get('after')||0),Number(url.searchParams.get('after')||0)+Number(url.searchParams.get('limit')||2000000)) }) });
      if (action === 'source' && req.method === 'GET') return json(res, 200, JSON.parse(readFileSync(join(dataDir, run.id, 'source.json'), 'utf8')));
      if (action === 'export' && req.method === 'GET') {
        res.setHeader('Content-Disposition', `attachment; filename="distvis-${run.id}.json"`);
        return json(res, 200, { schemaVersion: 1, ...run.info(), ...JSON.parse(readFileSync(join(dataDir, run.id, 'source.json'), 'utf8')), events: runEvents(run) });
      }
      if (action === 'events' && req.method === 'GET') {
        const after = Number(req.headers['last-event-id'] || url.searchParams.get('after') || 0);
        res.writeHead(200, { 'Content-Type': 'text/event-stream', 'Cache-Control': 'no-cache', 'Connection': 'keep-alive', 'X-Accel-Buffering': 'no' });
        const send = event => res.write(`id: ${event.seq}\ndata: ${JSON.stringify(event)}\n\n`);
        for (const event of runEvents(run)) if (event.seq > after) send(event);
        run.on('event', send);
        const heartbeat = setInterval(() => res.write(': keepalive\n\n'), 15000);
        req.on('close', () => { clearInterval(heartbeat); run.off('event', send); });
        return;
      }
      if(action==='checks' && req.method==='POST'){
        const result=await body(req);
        if(!['pass','fail','info'].includes(result.status)||typeof result.label!=='string')throw new Error('无效检查结果');
        run.log('check',result);persist(run);return json(res,200,{ok:true});
      }
      if (req.method === 'POST') {
        if (action === 'stop') {
          if (run.status === 'starting') throw new Error('节点正在构建，请等待启动完成');
          run.stop(); return json(res, 200, run.info());
        }
        if (action === 'faults') {
          if (run.mutating) return json(res, 409, { error: '上一项故障操作正在执行' });
          run.mutating = true;
          try {
            const fault = await body(req);
            if (runtimes.has(run.id)) await runtimes.get(run.id).fault(fault);
            else run.fault(fault);
            persist(run);
            return json(res, 200, { ok: true });
          } finally { run.mutating = false; }
        }
        if (action === 'commands') {
          const input = await body(req);
          if (input.batch !== undefined) return json(res, 200, { ok: true, ...run.commandBatch(input.batch) });
          const result = run.command(input.node, input.key, input.value, input.action, input.values);
          return json(res, 200, { ok: true, ...result });
        }
      }
    }
    if (url.pathname.startsWith('/api/')) return json(res, 404, { error: '接口不存在' });
    const doc = req.method === 'GET' && documentation(url.pathname);
    if (doc) {
      res.writeHead(200, {
        'Content-Type': doc.type,
        'Cache-Control': 'no-store',
        'Content-Security-Policy': "default-src 'self'; style-src 'self'; script-src 'self'; connect-src 'self'; img-src 'self' data:; object-src 'none'; base-uri 'none'; frame-ancestors 'none'",
      });
      return res.end(doc.body);
    }
    const file = { '/': 'index.html', '/app.js': 'app.js', '/style.css': 'style.css', '/docs.css': 'docs.css', '/docs.js': 'docs.js' }[url.pathname];
    if (!file || req.method !== 'GET') return json(res, 404, { error: '页面不存在' });
    res.writeHead(200, {
      'Content-Type': { '.html': 'text/html; charset=utf-8', '.js': 'text/javascript; charset=utf-8', '.css': 'text/css; charset=utf-8' }[extname(file)],
      'Content-Security-Policy': "default-src 'self'; style-src 'self' 'unsafe-inline'; script-src 'self'; connect-src 'self'; img-src 'self' data:; object-src 'none'; base-uri 'none'; frame-ancestors 'none'",
    });
    res.end(readFileSync(join(root, 'public', file)));
  } catch (error) {
    if (!res.headersSent) json(res, 400, { error: error.message });
    else res.end();
  }
});

const port = Number(process.env.PORT || 3000);
if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  server.on('error', error => { console.error(`无法启动本地服务: ${error.message}`); process.exitCode = 1; });
  server.listen(port, '127.0.0.1', () => console.log(`DistVis → http://localhost:${port}`));
}
async function shutdown() {
  if (shuttingDown) return;
  shuttingDown = true;
  clearInterval(timer);
  // Let bounded startup operations settle before cleanup, including partially created resources.
  await Promise.allSettled([...launches.values()]);
  for (const run of runs.values()) {
    if (run.status === 'running') {
      run.stop('coordinator 关闭');
      persist(run);
    }
  }
  await Promise.all([...runtimes.values()].map(r => r.cleanup()));
  server.close();
  process.exit(0);
}
process.on('SIGINT', shutdown);
process.on('SIGTERM', shutdown);
