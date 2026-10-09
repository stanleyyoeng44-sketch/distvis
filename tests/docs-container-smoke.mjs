// Builds and runs the downloadable documentation example unchanged in Docker.
// Requires an idle coordinator; only creates archived acceptance records.
import assert from 'node:assert/strict';
import {writeFile} from 'node:fs/promises';
const base=process.env.DISTVIS_URL || 'http://localhost:3000';
async function api(path,body){const r=await fetch(base+path,body===undefined?{}:{method:'POST',headers:{'Content-Type':'application/json','X-Distvis-Purpose':'acceptance'},body:JSON.stringify(body)});const d=await r.json();assert.ok(r.ok,JSON.stringify(d));return d;}
async function wait(check,timeout=600000){const end=Date.now()+timeout;while(Date.now()<end){const r=await check();if(r)return r;await new Promise(r=>setTimeout(r,300));}throw Error('timeout');}
assert.ok(!(await api('/api/runs')).some(r=>['starting','running'].includes(r.status)));
const files=await Promise.all(['main.go','go.mod'].map(async path=>({path,encoding:'utf8',content:await(await fetch(base+'/docs/examples/ping/'+path)).text()})));
const protocol=await api('/api/protocol-projects',{name:'文档 Ping 验收',protocol:'custom',projects:{shared:{files}}});
const experiment=await api('/api/experiments',{name:'Ping 双节点',protocolId:protocol.id,settings:{runtime:'docker',nodeCount:2,latency:80}});
const run=await api('/api/runs',{experimentId:experiment.id,name:'文档中的原样示例'}),path='/api/runs/'+run.id;
console.log('Ping run',run.id);
try{
 const ready=await wait(async()=>{const r=await api(path);assert.notEqual(r.status,'failed',JSON.stringify(r.events));return ['node-1','node-2'].every(n=>r.events.some(e=>e.type==='input_schema'&&e.node===n))&&r;});
 const schema=ready.events.findLast(e=>e.type==='input_schema'&&e.node==='node-1').schema;
 const input=schema.find(s=>s.label==='Application.Ping');assert.ok(input);
 for(const values of [{peer:'node-2',text:'hello'},{text:'默认后继'}]){
  const c=await api(path+'/commands',{node:'node-1',action:input.action,values});
  const result=await wait(async()=> (await api(path)).events.find(e=>e.type==='command_result'&&e.commandId===c.id),30000);
  assert.ok(!result.error,JSON.stringify(result));assert.deepEqual(result.result,{node:'node-2',text:values.text});
 }
 const r=await api(path);
 assert.ok(r.events.some(e=>e.type==='state'&&e.node==='node-2'&&e.state.received===2));
 assert.ok(r.events.some(e=>e.type==='send'&&e.payload?.method==='Echo.Echo'&&e.payload.body?.text==='hello'));
 console.log('PASS: documented Go program built and ran in Docker; RPC reply, payload capture, state report, implicit Neighbor');
}finally{if((await api(path)).status==='running')await api(path+'/stop',{});await writeFile('artifacts/docs-ping-acceptance.json',JSON.stringify(await api(path+'/export'),null,2));}
