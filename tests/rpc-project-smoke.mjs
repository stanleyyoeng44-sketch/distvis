// Run against an idle coordinator. Exercises real directory projects in Docker or K8s.
import assert from 'node:assert/strict';
import { execFile } from 'node:child_process';
import { promisify } from 'node:util';
import { writeFile } from 'node:fs/promises';
const exec=promisify(execFile),base=process.env.DISTVIS_URL||'http://localhost:3000',runtime=process.env.DISTVIS_RUNTIME||'docker';
async function api(path,data){
  const r=await fetch(base+path,data===undefined?{}:{method:'POST',headers:{'Content-Type':'application/json','X-Distvis-Purpose':'acceptance'},body:JSON.stringify(data)});
  const body=await r.json();assert.ok(r.ok,JSON.stringify(body));return body;
}
async function wait(check,label,timeout=30000){
  const end=Date.now()+timeout;
  while(Date.now()<end){const v=await check();if(v)return v;await new Promise(r=>setTimeout(r,250))}
  throw new Error('Timed out: '+label);
}
function states(run){return Object.fromEntries(run.events.filter(e=>e.type==='state').map(e=>[e.node,e.state]))}
assert.ok(!(await api('/api/runs')).some(r=>['running','starting'].includes(r.status)),'coordinator must be idle');
const library=await api('/api/library');
assert.equal(library.errors.length,0,JSON.stringify(library));
const results=[];
for(const folder of ['netrpc-token','grpc-broadcast']){
  const project=library.entries.find(p=>p.folder===folder);assert.ok(project,folder);
  const run=await api('/api/runs',{protocol:project.id,runtime,nodeCount:3,name:`RPC 目录验收 · ${runtime} · ${folder}`});
  const path=`/api/runs/${run.id}`,prefix=`distvis-${run.id.slice(0,8)}`;
  console.log('START',runtime,folder,run.id);
  try{
    await wait(async()=>{
      const r=await api(path);
      if(r.status==='failed')throw new Error(r.events.find(e=>e.level==='error')?.message||JSON.stringify(r));
      return r.status==='running';
    },'mounted project compilation',600000);
    const ready=await wait(async()=>{const r=await api(path);return new Set(r.events.filter(e=>e.type==='input_schema').map(e=>e.node)).size===3&&r},'RPC descriptors');
    const schemas=ready.events.filter(e=>e.type==='input_schema');
    assert.ok(schemas.every(e=>e.schema.length===1),'internal service must not be an application entry');
    const values=folder==='netrpc-token'?{Payload:'来自 RPC 表单'}:{number:'9223372036854775807',note:'RPC request / reply'};
    const action=schemas.find(e=>e.node==='node-3').schema[0].action;
    const input=await api(`${path}/commands`,{node:'node-3',action,values});
    const response=await wait(async()=>{const r=await api(path);return r.events.find(e=>e.type==='command_result'&&e.commandId===input.id)},'application RPC response');
    assert.ok(!response.error,JSON.stringify(response));
    if(folder==='netrpc-token'){
      assert.equal(response.result.Queued,true);
      await wait(async()=>states(await api(path))['node-3']?.lastProcessed==='来自 RPC 表单','queued payload waits for token');
      await wait(async()=>(await api(path)).events.some(e=>e.type==='receive'&&e.payload?.body?.Payload==='来自 RPC 表单'),'token carries payload via RPC');
    }else{
      assert.equal(response.result.number,'9223372036854775807','int64 remains exact');
      await wait(async()=>Object.values(states(await api(path))).filter(s=>s.number==='9223372036854775807').length===3,'generated gRPC client broadcasts');
      await api(`${path}/faults`,{kind:'link',from:'node-3',to:'node-1',blocked:true,bidirectional:true,latency:80,bandwidth:128});
      await api(`${path}/commands`,{node:'node-3',action,values:{number:'99',note:'partition'}});
      await wait(async()=>(await api(path)).events.some(e=>e.type==='drop'&&e.payload?.method==='/lesson.Replication/Apply'),'RPC request obeys link fault');
      assert.equal(states(await api(path))['node-1'].number,'9223372036854775807');
      await api(`${path}/faults`,{kind:'heal'});
      await api(`${path}/faults`,{kind:'crash',node:'node-1'});
      await api(`${path}/faults`,{kind:'recover',node:'node-1'});
      await wait(async()=>{
        const r=await api(path);
        const recovered=r.events.findLast(e=>e.type==='node'&&e.node==='node-1'&&e.online);
        return r.events.some(e=>e.type==='state'&&e.node==='node-1'&&e.seq>recovered?.seq);
      },'directory node recovery');
      const next=await api(`${path}/commands`,{node:'node-1',action,values:{number:'7',note:'after recovery'}});
      await wait(async()=>(await api(path)).events.some(e=>e.type==='command_result'&&e.commandId===next.id&&!e.error),'RPC after process recovery');
    }
    if(runtime==='kubernetes'){
      const {stdout}=await exec('kubectl',['--context','docker-desktop','get','pod','node-1','-n',prefix,'-o','json']);
      const pod=JSON.parse(stdout);
      assert.equal(pod.status.initContainerStatuses[0].state.terminated.exitCode,0,'Pod compiles its mounted snapshot');
      assert.ok(pod.spec.volumes.some(v=>v.name==='library'&&v.hostPath));
      assert.ok(pod.spec.containers[0].volumeMounts.some(v=>v.mountPath==='/protocols'&&v.readOnly));
      assert.equal(pod.spec.containers[0].workingDir,'/protocol');
      const {stdout:mod}=await exec('kubectl',['--context','docker-desktop','exec','-n',prefix,'node-1','--','cat','/protocol/go.mod']);
      assert.ok(mod.includes('module lesson/'));
    }else{
      const {stdout}=await exec('docker',['inspect',`${prefix}-node-1`]);
      const mounts=JSON.parse(stdout)[0].Mounts;
      assert.ok(mounts.some(m=>m.Destination==='/protocols'&&m.Source===library.root&&!m.RW));
      assert.ok(mounts.some(m=>m.Destination==='/protocol'&&!m.RW));
    }
    const recorded=await api(path);
    assert.ok(recorded.events.some(e=>e.type==='receive'&&e.payload?.type==='RPCResponse'));
    assert.ok(recorded.events.some(e=>e.type==='send'&&e.payload?.type==='RPCRequest'));
    await api(`${path}/stop`,{});
    const archive=await api(`${path}/export`);
    assert.equal(archive.project.sha256,archive.config.project.sha256);
    assert.ok(archive.project.files.some(f=>f.path==='go.mod'));
    assert.ok(archive.project.files.some(f=>f.path==='main.go'));
    if(folder==='grpc-broadcast')assert.ok(archive.project.files.some(f=>f.path.endsWith('.proto')));
    results.push({runtime,folder,id:run.id,status:'passed'});
    console.log('PASS',folder);
  }finally{
    if((await api(path)).status==='running')await api(`${path}/stop`,{});
    await wait(async()=>{
      const args=runtime==='kubernetes'?['--context','docker-desktop','get','namespace',prefix,'--ignore-not-found','-o','name']:['ps','-a','--filter',`label=distvis.run=${run.id}`,'--format','{{.Names}}'];
      const {stdout}=await exec(runtime==='kubernetes'?'kubectl':'docker',args);return !stdout.trim();
    },'resource cleanup',90000);
  }
}
await writeFile(`artifacts/rpc-${runtime}-acceptance.json`,JSON.stringify({checkedAt:new Date().toISOString(),results},null,2));
