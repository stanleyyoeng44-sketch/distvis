// Actual agent workflow over stdio MCP against an idle local coordinator/Docker.
import assert from 'node:assert/strict';
import {readFile,writeFile,unlink,rmdir} from 'node:fs/promises';
import {join} from 'node:path';
import {fileURLToPath} from 'node:url';
import {Client} from '@modelcontextprotocol/sdk/client/index.js';
import {StdioClientTransport} from '@modelcontextprotocol/sdk/client/stdio.js';
const base=process.env.DISTVIS_URL || 'http://localhost:3000';
const client=new Client({name:'distvis-agent-acceptance',version:'1.0.0'});
const transport=new StdioClientTransport({command:process.execPath,args:[fileURLToPath(new URL('../mcp/server.js',import.meta.url))],env:{PATH:process.env.PATH,DISTVIS_URL:base},stderr:'inherit'});
async function call(name,args={}){const r=await client.callTool({name,arguments:args});assert.ok(!r.isError,JSON.stringify(r));return r.structuredContent;}
async function wait(check,ms=600000){const until=Date.now()+ms;while(Date.now()<until){const value=await check();if(value)return value;await new Promise(r=>setTimeout(r,300));}throw Error('timeout');}
let scaffold,run;
try{
 await client.connect(transport);
 assert.equal((await call('distvis_catalog')).activeRuns.length,0,'coordinator must be idle');
 scaffold=await call('distvis_scaffold_protocol',{folder:`agent-check-${Date.now()}`,name:'Agent MCP 验收'});
 const file=join(scaffold.directory,'main.go');
 await writeFile(file,(await readFile(file,'utf8')).replace('Text: args.Text}', 'Text: "agent:" + args.Text}'));
 const protocol=await call('distvis_import_protocol',{directoryId:scaffold.directoryId,acceptance:true});
 const experiment=await call('distvis_create_experiment',{protocolId:protocol.id,name:'MCP 双节点输入与故障',nodeCount:2});
 run=await call('distvis_start_run',{experimentId:experiment.id,name:'Agent 实现代码后运行'});
 console.log('run',run.id);
 const inspect=(extra={})=>call('distvis_inspect_run',{runId:run.id,...extra});
 const ready=await wait(async()=>{const r=await inspect();assert.notEqual(r.status,'failed',JSON.stringify(r.events));return r.status==='running' && ['node-1','node-2'].every(id=>r.nodes[id]?.inputs?.length) && r;});
 const input=ready.nodes['node-1'].inputs.find(i=>i.label==='Application.Ping');assert.ok(input);
 await call('distvis_fault',{runId:run.id,fault:{kind:'link',from:'node-1',to:'node-2',latency:400,bandwidth:128,blocked:false,bidirectional:true}});
 const command=await call('distvis_input',{runId:run.id,node:'node-1',action:input.action,values:{peer:'node-2',text:'MCP'}});
 const result=await wait(async()=> (await inspect({types:['command_result']})).events.find(e=>e.commandId===command.id),30000);
 assert.ok(!result.error);assert.deepEqual(result.result,{node:'node-2',text:'agent:MCP'});
 assert.equal((await inspect()).nodes['node-2'].state.received,1);
 await call('distvis_fault',{runId:run.id,fault:{kind:'crash',node:'node-2'}});
 assert.equal((await inspect()).nodes['node-2'].online,false);
 await call('distvis_fault',{runId:run.id,fault:{kind:'recover',node:'node-2'}});
 await call('distvis_fault',{runId:run.id,fault:{kind:'heal'}});
 assert.equal((await inspect()).nodes['node-2'].online,true);
 await call('distvis_stop_run',{runId:run.id});
 const snapshot=await inspect();assert.equal(snapshot.status,'completed');
 const all=[];let after=0,page;
 do{page=await inspect({after,limit:3});all.push(...page.events);after=page.nextAfter;}while(page.hasMore);
 assert.equal(all.length,snapshot.eventCount);assert.equal(new Set(all.map(e=>e.seq)).size,all.length);
 const archive=await (await fetch(snapshot.exportUrl)).json();
 assert.ok(archive.events.some(e=>e.type==='send'&&e.payload?.type==='RPCRequest'&&e.delay>=400));
 await writeFile('artifacts/mcp-acceptance.json',JSON.stringify(archive,null,2));
 console.log('PASS: MCP scaffold → edit → import → Docker build/run → typed input/reply → state → link/crash/recover/heal → stop → paginated history');
}finally{
 if(run){const r=await call('distvis_inspect_run',{runId:run.id});if(r.status==='running')await call('distvis_stop_run',{runId:run.id});}
 await client.close();
 if(scaffold){for(const file of scaffold.files)await unlink(join(scaffold.directory,file));await rmdir(scaffold.directory);}
}
