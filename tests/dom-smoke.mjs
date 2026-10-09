// Optional DOM interaction test. Requires linkedom; no browser or listening port.
import { fileURLToPath } from 'node:url';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
const { parseHTML } = await import(process.env.DISTVIS_DOM_MODULE || 'linkedom');
import {readFileSync, mkdtempSync} from 'node:fs';
import {Readable, Writable} from 'node:stream';
import vm from 'node:vm';
import assert from 'node:assert/strict';
const root=fileURLToPath(new URL('../', import.meta.url)).replace(/\/$/, '');
process.env.DISTVIS_DATA=mkdtempSync(join(tmpdir(), 'distvis-ui-'));
const {server}=await import(root+'/server/index.js');
const {window, document}=parseHTML(readFileSync(root+'/public/index.html','utf8'));
for(const el of document.querySelectorAll('select')){
 Object.defineProperty(el,'value',{get(){return this._value??this.querySelector('option[selected]')?.value??this.querySelector('option')?.value??''},set(v){this._value=String(v)}});
 Object.defineProperty(el,'selectedIndex',{set(v){this.value=this.querySelectorAll('option')[v]?.value??''}});
}
for(const el of document.querySelectorAll('form'))Object.defineProperty(el,'elements',{get(){return Object.fromEntries([...this.querySelectorAll('[name]')].map(n=>[n.name,n]))}});
for(const el of document.querySelectorAll('dialog')){el.showModal=()=>el.setAttribute('open','');el.close=()=>el.removeAttribute('open');}
for(const el of document.querySelectorAll('input[type=checkbox]'))el.checked=el.hasAttribute('checked');
const store=new Map();let frame;const streams=[];const intervals=[];const errors=[];
const request=(path,options={})=>new Promise(resolve=>{
 const req=Readable.from(options.body?[options.body]:[]);Object.assign(req,{method:options.method||'GET',url:path,headers:{host:'localhost:3000'}});
 let text='';const res=new Writable({write(c,e,done){text+=c;done()}});res.setHeader=()=>{};res.writeHead=(s)=>{res.statusCode=s;res.headersSent=true};
 res.on('finish',()=>resolve({ok:res.statusCode<400,status:res.statusCode,json:async()=>JSON.parse(text)}));server.emit('request',req,res);
});
const context={document,window,console,structuredClone,TextDecoder,Uint8Array,atob,fetch:request,localStorage:{getItem:k=>store.get(k)||null,setItem:(k,v)=>store.set(k,v),removeItem:k=>store.delete(k)},setTimeout,clearTimeout,setInterval:(f)=>{intervals.push(f);return 1},requestAnimationFrame:f=>{frame=f},performance,location:{},MouseEvent:window.Event,Event:window.Event,FormData:class{constructor(form){this.data=Object.entries(form.elements).map(([k,v])=>[k,v.value])}*[Symbol.iterator](){yield* this.data}get(k){return this.data.find(v=>v[0]===k)?.[1]}},EventSource:class{constructor(url){this.url=url;streams.push(this)}close(){this.closed=true}}};
vm.createContext(context);vm.runInContext(readFileSync(root+'/public/app.js','utf8'),context);
// Controlled replies: held requests resolve only when a test releases them, possibly with stale data or a failure.
const hold={get:false,post:false},heldGets=[],heldPosts=[];
context.fetch=(path,options={})=>{
 if(options.method==='POST'?hold.post:hold.get)return new Promise(resolve=>(options.method==='POST'?heldPosts:heldGets).push({path,options,resolve}));
 return request(path,options);
};
const failReply={ok:false,status:500,json:async()=>({error:'模拟失败'})};
async function releasePost(ok=true){const {path,options,resolve}=heldPosts.shift();resolve(ok?await request(path,options):failReply);await delay();}
async function releaseGets(patch=(path,data)=>data){while(heldGets.length){const {path,resolve}=heldGets.shift();const data=patch(path,await (await request(path)).json());resolve({ok:true,status:200,json:async()=>data});}await delay();}
let frameClock=0; const paint=()=>frame?.(frameClock+=200); const delay=()=>new Promise(r=>setTimeout(r,100));
const click=async selector=>{const el=document.querySelector(selector);if(el.onclick)await el.onclick({target:el,preventDefault(){}});else el.dispatchEvent(new window.Event('click',{bubbles:true}));await delay();paint();};
async function sync(){for(const stream of streams.filter(s=>!s.closed)){const path=stream.url.split('/events')[0];const result=await (await request(path)).json();for(const e of result.events)stream.onmessage?.({data:JSON.stringify(e)});}paint();}
await delay();
assert.match(document.querySelector('#connection-status').textContent,/已连接/);
await click('#create-experiment');
const projectForm=document.querySelector('#project-form');
await projectForm.onsubmit({target:projectForm,preventDefault(){}});await delay();
assert.equal(document.querySelector('#code-dialog').hidden,false);
assert.equal(document.querySelector('#code-example'),null,'code choice belongs to experiment creation only');
await click('#workspace-new-experiment');
const childForm=document.querySelector('#new-form');
childForm.elements.name.value='Raft 参数实验';
await childForm.onsubmit({target:childForm,preventDefault(){}});await delay();
const experimentId=vm.runInContext('experiment.id',context);
const protocolId=vm.runInContext('protocolProject.id',context);
await click('#new-run');assert.ok(document.querySelector('#new-dialog').hasAttribute('open'));
const form=document.querySelector('#new-form');
assert.equal(form.elements.runtime.value, 'docker', 'real Go execution remains the default');
form.elements.runtime.value = 'simulation';
await form.onsubmit({target:form,preventDefault(){}});await delay();await sync();
assert.match(document.querySelector('#experiment-name').textContent,/Raft/);
assert.equal(document.querySelectorAll('.graph-node').length,5);
assert.ok(Number(document.querySelector('#event-count').textContent)>0);
await click('#play');await click('#rewind');assert.equal(document.querySelector('#event-count').textContent,'0');
assert.equal(document.querySelector('#metric-messages').textContent,'0');
await click('#step');assert.equal(document.querySelector('#event-count').textContent,'1');
await click('#go-live');
assert.equal(document.querySelector('.inspector-tabs'),null,'node details and faults share one popover');
assert.equal(document.querySelector('#fault-kind'),null,'the fault target comes from the selection');
assert.equal(document.querySelector('#element-popover').hidden,true,'opening a run does not open the popover');
await click('#graph [data-node="node-1"]');
assert.equal(document.querySelector('#element-popover').hidden,false);
assert.equal(document.querySelector('#popover-title').textContent,'node-1');
assert.equal(document.querySelector('#node-toggle').dataset.nodeAction,'crash');
await click('#node-toggle');await sync();assert.equal(document.querySelectorAll('.node-offline').length,1);
assert.equal(document.querySelector('#node-toggle').dataset.nodeAction,'recover','the toggle follows the live node state');
await click('#node-toggle');await sync();assert.equal(document.querySelectorAll('.node-offline').length,0);
// Editing one direction of a link leaves the other direction's rule untouched.
await click('#peer-links [data-peer="node-2"]');
assert.equal(document.querySelector('#popover-title').textContent,'node-1 ↔ node-2');
assert.equal(document.querySelector('#dir-ab').getAttribute('aria-pressed'),'true','peer rows default to selected node → peer');
document.querySelector('#link-latency').value='900';document.querySelector('#link-blocked').checked=true;
await document.querySelector('#link-form').dispatchEvent(new window.Event('submit',{bubbles:true,cancelable:true}));await delay();await sync();
assert.match(document.querySelector('#link-rules [data-direction="ab"]').textContent,/中断/);
assert.match(document.querySelector('#link-rules [data-direction="ba"]').textContent,/默认/,'reverse direction untouched');
await click('#dir-ba');
document.querySelector('#link-latency').value='250';document.querySelector('#link-blocked').checked=false;
await document.querySelector('#link-form').dispatchEvent(new window.Event('submit',{bubbles:true,cancelable:true}));await delay();await sync();
assert.match(document.querySelector('#link-rules [data-direction="ba"]').textContent,/250 ms/);
assert.match(document.querySelector('#link-rules [data-direction="ab"]').textContent,/中断/,'forward rule preserved');
await click('#dir-both');
assert.equal(document.querySelector('#link-notice').hidden,false);
assert.match(document.querySelector('#link-notice').textContent,/覆盖/,'differing rules warn before a two-way overwrite');
document.querySelector('#link-latency').value='';
await document.querySelector('#link-form').dispatchEvent(new window.Event('submit',{bubbles:true,cancelable:true}));await delay();
assert.match(document.querySelector('#popover-error').textContent,/整数/,'blank values are rejected before the request');
await click('#heal-links');await sync();
assert.match(document.querySelector('#link-rules').textContent,/默认[\s\S]*默认/,'heal resets both directions');
await click('#popover-back');
assert.equal(document.querySelector('#popover-title').textContent,'node-1','back returns to the node the link was opened from');
await click('#popover-close');
assert.equal(document.querySelector('#element-popover').hidden,true);
// Controls read the live run even while the replay shows an earlier moment.
await click('#graph [data-node="node-1"]');
await click('#node-toggle');await sync();
vm.runInContext('seek(1)',context);paint();
assert.equal(document.querySelectorAll('.node-offline').length,0,'replay shows node-1 online at the start');
assert.equal(document.querySelector('#node-toggle').dataset.nodeAction,'recover','the toggle still reflects the live crash');
assert.match(document.querySelector('#node-panel .inspector-section-title').textContent,/回放 @/,'replay state is labelled');
await click('#go-live');
await click('#node-toggle');await sync();
assert.equal(document.querySelectorAll('.node-offline').length,0);
// Live events never rebuild a form the student is typing into.
const numberInput=()=>document.querySelector('#application-panel input[name="number"]');
const typing=numberInput();assert.ok(typing,'the reference model declares a proposal input');
typing.value='77';typing.dispatchEvent(new window.Event('input',{bubbles:true}));
await sync();paint();
assert.equal(numberInput(),typing,'form element survives live updates');assert.equal(numberInput().value,'77');
// A late reply for node-1 stays silent once the user has moved to node-2, and only clears its own pending flag.
document.querySelector('#toast').textContent='';
vm.runInContext("pendingToggle=document.querySelector('#node-toggle').onclick();openNodePopover('node-2');",context);
await vm.runInContext('pendingToggle',context);await delay();await sync();
assert.doesNotMatch(document.querySelector('#toast').textContent,/node-1/,'no notice for a target the user left');
assert.equal(document.querySelector('#popover-title').textContent,'node-2');
assert.equal(vm.runInContext('pending.size',context),0);
await click('#graph [data-node="node-1"]');await click('#node-toggle');await sync();
assert.equal(document.querySelectorAll('.node-offline').length,0);
// A link edit typed while an earlier apply is in flight is newer than what was sent and survives the reply.
await click('#peer-links [data-peer="node-3"]');
const latency=document.querySelector('#link-latency');
latency.value='300';latency.dispatchEvent(new window.Event('input',{bubbles:true}));
document.querySelector('#link-form').dispatchEvent(new window.Event('submit',{bubbles:true,cancelable:true}));
latency.value='450';latency.dispatchEvent(new window.Event('input',{bubbles:true}));
await delay();await sync();
assert.match(document.querySelector('#link-rules [data-direction="ab"]').textContent,/300 ms/,'the submitted value was applied');
assert.equal(latency.value,'450','an edit typed during the request is kept');
assert.equal(document.querySelector('#link-changed').hidden,false,'the live change is reported against the baseline the edit started from');
await click('#link-use-latest');
assert.equal(latency.value,'300');assert.equal(document.querySelector('#link-changed').hidden,true);
// After the user leaves the run, a late reply stays silent.
document.querySelector('#toast').textContent='';
vm.runInContext("pendingHeal=document.querySelector('#heal-links').onclick();loadingRun++;",context);
await vm.runInContext('pendingHeal',context);await delay();await sync();
assert.equal(document.querySelector('#toast').textContent,'','no notice for a run the user left');
assert.equal(vm.runInContext('pending.size + faultsInFlight.size',context),0,'pending state is released');
// A late failure for one input action must not appear after the user switched to another action.
vm.runInContext("savedSchemas=liveState.schemas['node-1'];liveState.schemas['node-1']=[{action:'first',label:'First',fields:[]},{action:'second',label:'Second',fields:[]}];schemaRev++;openNodePopover('node-1');",context);
document.querySelector('#toast').textContent='';hold.post=true;
document.querySelector('#application-panel form[data-action="first"]').dispatchEvent(new window.Event('submit',{bubbles:true,cancelable:true}));
await delay();hold.post=false;
vm.runInContext("inputActions.set(inputDraftKey('node-1'),'second');renderDirty=true;render();",context);
await releasePost(false);paint();
assert.equal(document.querySelector('#popover-error').hidden,true,'no stale error for the action the user left');
assert.equal(document.querySelector('#toast').textContent,'','no stale notice for the action the user left');
vm.runInContext("inputActions.set(inputDraftKey('node-1'),'first');renderDirty=true;render();",context);
assert.equal(document.querySelector('#popover-error').hidden,true);
vm.runInContext("liveState.schemas['node-1']=savedSchemas;schemaRev++;renderDirty=true;render();",context);
// Same for a link direction: a late failure for node-1 → node-2 stays silent once node-2 → node-1 is shown.
await click('#peer-links [data-peer="node-2"]');
hold.post=true;document.querySelector('#link-form').dispatchEvent(new window.Event('submit',{bubbles:true,cancelable:true}));
await delay();hold.post=false;
await click('#dir-ba');
await releasePost(false);paint();
assert.equal(document.querySelector('#popover-error').hidden,true,'no stale error for the direction the user left');
assert.equal(document.querySelector('#toast').textContent,'');
// A dirty draft survives a success that arrives after the user moved to another element and then another run.
await click('#dir-ab');
document.querySelector('#link-latency').value='777';document.querySelector('#link-latency').dispatchEvent(new window.Event('input',{bubbles:true}));
hold.post=true;document.querySelector('#link-form').dispatchEvent(new window.Event('submit',{bubbles:true,cancelable:true}));
await delay();hold.post=false;
vm.runInContext("openNodePopover('node-3');loadingRun++;",context);
await releasePost(true);await sync();
vm.runInContext("openNodePopover('node-1');openLinkPopover('node-1','node-2','node');",context);paint();
assert.equal(document.querySelector('#link-latency').value,'777','the stale success did not retire the draft');
assert.equal(document.querySelector('#link-changed').hidden,false,'the applied rule is reported as a live change');
await click('#link-use-latest');
// A two-way draft watches both directions: a change to the reverse rule alone is reported.
await click('#dir-both');
document.querySelector('#link-latency').value='555';document.querySelector('#link-latency').dispatchEvent(new window.Event('input',{bubbles:true}));
vm.runInContext("liveState.links['node-2>node-1']={latency:99,bandwidth:5,blocked:false};schemaRev++;renderDirty=true;render();",context);
assert.equal(document.querySelector('#link-changed').hidden,false,'reverse-only change is noticed');
document.querySelector('#link-latency').value='556';document.querySelector('#link-latency').dispatchEvent(new window.Event('input',{bubbles:true}));paint();
assert.equal(document.querySelector('#link-latency').value,'556');
assert.equal(document.querySelector('#link-changed').hidden,false,'the notice survives further typing');
await click('#link-use-latest');
assert.equal(document.querySelector('#link-changed').hidden,true,'use latest resets the baseline');
assert.notEqual(document.querySelector('#link-latency').value,'556');
// The redraw stamp carries the whole selection, link direction included.
const stampNow=()=>vm.runInContext('graphStamp',context);
await click('#dir-ab');const abStamp=stampNow();
await click('#dir-ba');const baStamp=stampNow();
await click('#dir-both');const bothStamp=stampNow();
assert.equal(new Set([abStamp,baStamp,bothStamp]).size,3,'each link direction is a distinct selection');
assert.match(bothStamp,/"dir":"both"/);
vm.runInContext("delete liveState.links['node-2>node-1'];schemaRev++;closePopover();",context);
assert.match(stampNow(),/:null:/,'a closed popover is part of the stamp');
// A stop request that fails after the user left the run stays silent.
document.querySelector('#toast').textContent='';
hold.post=true;const stopRequest=vm.runInContext("document.querySelector('#stop-run').onclick()",context);await delay();hold.post=false;
vm.runInContext('loadingRun++',context);
await releasePost(false);await stopRequest;paint();
assert.equal(document.querySelector('#toast').textContent,'','no stale stop failure notice');
assert.equal(vm.runInContext('stopsInFlight.size',context),0,'the stop button is released');
// Busy state belongs to the run that sent the request: a slow fault or stop reply from this run
// does not disable the controls of another running run shown meanwhile.
hold.post=true;
const slowFault=vm.runInContext("document.querySelector('#heal-links').onclick()",context);await delay();
const slowStop=vm.runInContext("document.querySelector('#stop-run').onclick()",context);await delay();
hold.post=false;
assert.equal(document.querySelector('#heal-links').disabled,true,'the sending run shows its own busy state');
vm.runInContext("busyRun=run;run={...run,id:'next-run',status:'running'};openNodePopover('node-1');",context);paint();
assert.equal(document.querySelector('#heal-links').disabled,false,'another run keeps its fault controls');
assert.equal(document.querySelector('#node-toggle').disabled,false);
assert.equal(document.querySelector('#stop-run').disabled,false,'another run keeps its stop button');
assert.equal(document.querySelector('#stop-run').textContent.trim(),'结束实验');
while(heldPosts.length)await releasePost(false);await slowFault;await slowStop;
assert.equal(vm.runInContext('faultsInFlight.size + stopsInFlight.size',context),0,'each request cleared only its own run');
vm.runInContext("run=busyRun;closePopover();renderDirty=true;render();",context);
// A poll that started while this run was active must not revive it after it ended and the user left it.
const endedRunId=vm.runInContext('run.id',context);
hold.get=true;const stalePoll=vm.runInContext('refreshRuns()',context);await delay();hold.get=false;
await request(`/api/runs/${endedRunId}/stop`,{method:'POST',body:'{}'});await sync();
assert.equal(vm.runInContext('run.status',context),'completed');
vm.runInContext('clearRun()',context);
const asRunning=(path,data)=>path==='/api/experiments'?data.map(e=>e.latestRun?.id===endedRunId?{...e,latestRun:{...e.latestRun,status:'running'}}:e):Array.isArray(data)?data.map(r=>r.id===endedRunId?{...r,status:'running'}:r):data;
await releaseGets(asRunning);await stalePoll;paint();
assert.equal(vm.runInContext(`experiments.find(e=>e.id===experiment.id).latestRun.status`,context),'completed','the stale poll did not revive the ended run');
assert.equal(vm.runInContext('runControlState().mode',context),'run');
// An older poll is dropped when a newer one has already been applied; a genuinely new running ID is still found.
hold.get=true;const olderPoll=vm.runInContext('refreshRuns()',context);await delay();const older=heldGets.splice(0);hold.get=false;
await vm.runInContext('refreshRuns()',context);
heldGets.push(...older);await releaseGets((path,data)=>path==='/api/experiments'?data.map(e=>({...e,name:'stale'})):data);await olderPoll;
assert.notEqual(vm.runInContext('experiments.find(e=>e.id===experiment.id).name',context),'stale','older responses are discarded');
hold.get=true;const freshPoll=vm.runInContext('refreshRuns()',context);await delay();hold.get=false;
await releaseGets((path,data)=>path==='/api/experiments'?data.map(e=>e.id===vm.runInContext('experiment.id',context)?{...e,latestRun:{id:'brand-new-run',status:'running',createdAt:new Date().toISOString()}}:e):data);await freshPoll;paint();
assert.equal(vm.runInContext('runControlState().mode',context),'goto','a new running ID is discoverable');
await vm.runInContext('refreshRuns()',context);
await vm.runInContext(`openRun('${endedRunId}')`,context);await delay();paint();
await click('#stop-run');await sync();assert.equal(document.querySelector('#run-status').textContent,'已结束');
assert.equal(document.querySelector('#new-run').textContent.trim(),'重新运行','an ended run offers to run again');
// Another active run of the same experiment wins over "run again", and a replay never stops it.
vm.runInContext("experiments.find(e=>e.id===experiment.id).latestRun={id:'another-run',status:'running'};renderDirty=true;render();",context);
assert.equal(document.querySelector('#new-run').textContent.trim(),'前往运行中的实验');
assert.equal(document.querySelector('#stop-run').hidden,true);
vm.runInContext("experiments.find(e=>e.id===experiment.id).latestRun={id:run.id,status:'completed'};renderDirty=true;render();",context);
// A startup failure arrives as a lifecycle event; a late start event cannot revive the run.
vm.runInContext("endedRun=run;run={...run,id:'failing-run',status:'starting'};receiveEvent({seq:events.at(-1).seq+1,time:latestTime,type:'lifecycle',action:'failed'});receiveEvent({seq:events.at(-1).seq+1,time:latestTime,type:'lifecycle',action:'start'});render();",context);
assert.equal(vm.runInContext('run.status',context),'failed');
assert.equal(document.querySelector('#run-status').textContent,'启动失败');
assert.equal(document.querySelector('#view-error').hidden,false);
vm.runInContext("receiveEvent({seq:events.at(-1).seq+1,time:latestTime,type:'lifecycle',action:'stop'});",context);
assert.equal(vm.runInContext('run.status',context),'failed','the first terminal status wins');
vm.runInContext("run=endedRun;renderDirty=true;render();",context);
await click('#nav-history');assert.equal(document.querySelectorAll('.history-row').length,1);
const historyButton=document.querySelector('[data-history]');await document.querySelector('#history-list').onclick({target:historyButton});await delay();paint();assert.equal(document.querySelector('#event-count').textContent,'0');
await click('#nav-code');assert.match(document.querySelector('#source-editor').value,/lab.Main/);
assert.ok(document.querySelector('[data-file="protocol.go"]'));
await click('[data-file="protocol.go"]');
assert.match(document.querySelector('#source-editor').value,/func .*RequestVote/);
const editor=document.querySelector('#source-editor');
editor.value+='\n// 文件切换保留修改\n';editor.oninput({target:editor});
await click('[data-file="main.go"]');await click('[data-file="protocol.go"]');
assert.match(editor.value,/文件切换保留修改/);
await click('#save-code');
const saved=(await (await request('/api/protocol-projects/'+protocolId)).json()).projects.shared;
assert.match(Buffer.from(saved.files.find(f=>f.path==='protocol.go').content,'base64').toString(),/文件切换保留修改/);
await vm.runInContext(`openExperiment('${experimentId}')`,context);
await click('#view-run-source');assert.equal(editor.readOnly,true);
await click('#return-code-draft');assert.equal(editor.readOnly,false);
assert.equal(document.querySelectorAll('script').length,1);
await vm.runInContext(`openExperiment('${experimentId}', 'visual')`,context);

// A recorded prefix with identical timestamps tests causality by sequence, not just time.
context.fixture = [
 {seq:1,time:0,type:'lifecycle',action:'start'},
 {seq:2,time:0,type:'state',node:'node-1',state:{role:'candidate',term:3,votedFor:'node-1'}},
 {seq:3,time:0,type:'state',node:'node-2',state:{role:'follower',term:3,votedFor:null}},
 {seq:4,time:100,type:'send',id:'m1',from:'node-1',to:'node-2',delay:80,payload:{type:'Vote',term:3,granted:false,text:'<img src=x onerror=alert(1)>',nested:{entry:'proposal'}}},
 {seq:5,time:180,type:'receive',id:'m1',from:'node-1',to:'node-2',payload:{type:'Vote',term:3,granted:false}},
 {seq:6,time:180,type:'state',node:'node-2',state:{role:'candidate',term:4,votedFor:'node-2'}},
 {seq:7,time:200,type:'send',id:'m2',from:'node-2',to:'node-1',delay:80,payload:{type:'RequestVote',term:4}},
 {seq:8,time:200,type:'drop',id:'m2',from:'node-2',to:'node-1',reason:'链路中断'},
 {seq:9,time:300,type:'send',id:'m3',from:'node-1',to:'node-1',delay:100,payload:{type:'Self',round:0}},
 {seq:10,time:500,type:'lifecycle',action:'stop'}
];
vm.runInContext('events=[]; ends.clear(); sends.clear(); resetCache(); latestTime=0; live=false; fixture.forEach(receiveEvent); run.status="completed"; seek(4);', context);
assert.ok(document.querySelector('.packet-arrow'), 'messages use an arrow shape');
assert.match(document.querySelector('.message-label').textContent, /term=3/);
assert.match(document.querySelector('.packet').getAttribute('aria-label'), /node-1 → node-2/);
await click('.packet');
assert.equal(document.querySelector('#event-title').textContent, '消息详情');
assert.equal(document.querySelector('#event-count').textContent, '4', 'inspecting a packet preserves the playhead');
assert.match(document.querySelector('#event-summary').textContent, /尚未接收/);
assert.equal(document.querySelector('#event-summary [data-field="granted"] strong').textContent, 'false');
assert.match(document.querySelector('#event-summary [data-field="nested"]').textContent, /proposal/);
assert.equal(document.querySelector('#event-summary img'), null, 'payload HTML must be escaped');
assert.match(document.querySelector('#event-summary').textContent, /<img/);
document.querySelector('#event-dialog').close();

await click('[data-view="spacetime"]');
assert.equal(document.querySelector('#graph').hasAttribute('hidden'), true);
assert.equal(document.querySelector('#spacetime-panel').hidden, false);
assert.equal(document.querySelectorAll('#spacetime .time-node').length, 5);
assert.equal(document.querySelector('[data-message-id="m1"]').dataset.status, 'pending', 'future receive not leaked');
assert.equal(document.querySelector('[data-message-id="m2"]'), null, 'future send not leaked');
assert.ok(document.querySelector('.time-message-line').getAttribute('marker-end'), 'time-space graph has directed edges');
await click('#spacetime [data-node="node-2"]');
assert.equal(document.querySelector('#node-panel [data-field="term"] strong').textContent, '3');
await click('#step'); // receive at t=180, before state transition at the same t
assert.equal(document.querySelector('[data-message-id="m1"]').dataset.status, 'received');
assert.equal(document.querySelector('#node-panel [data-field="term"] strong').textContent, '3', 'equal timestamp future state hidden');
await click('.time-message');
assert.match(document.querySelector('#event-summary').textContent, /已接收/);
assert.equal(document.querySelector('#event-count').textContent, '5', 'inspection does not jump back to send');
document.querySelector('#event-dialog').close();
await click('#step');
assert.equal(document.querySelector('#node-panel [data-field="term"] strong').textContent, '4');
vm.runInContext('seek(8)', context);
assert.equal(document.querySelector('[data-message-id="m2"]').dataset.status, 'dropped');
assert.ok(document.querySelector('.time-cross'));
vm.runInContext('seek(9)', context);
assert.match(document.querySelector('[data-message-id="m3"] .time-message-line').getAttribute('d'), /C/, 'self messages get a loop');
await click('[data-view="topology"]');
assert.equal(document.querySelector('#event-count').textContent, '9', 'switching views preserves replay position');
assert.equal(document.querySelectorAll('.packet-arrow').length > 0, true);

await click('#rewind');
const rate = document.querySelector('#speed');
for (const speed of ['0.01','0.025','0.05','0.1']) assert.ok(rate.querySelector(`option[value="${speed}"]`));
rate.value='0.01'; rate.onchange({target:rate});
await click('#play');
const startTime = vm.runInContext('playTime', context);
for(let i=0;i<5;i++)paint(); // 1 second of wall clock -> 10ms of recorded time
assert.ok(Math.abs(vm.runInContext('playTime', context) - startTime - 10) < 0.001, '0.01x changes replay clock');
await click('#play');
const pausedTime = vm.runInContext('playTime', context); paint();
assert.equal(vm.runInContext('playTime', context), pausedTime, 'pause freezes replay clock');
await click('#go-live');
rate.value='0.05';rate.onchange({target:rate});
assert.equal(vm.runInContext('live', context), false, 'choosing speed activates replay instead of staying live');

// A packet crossing the entire viewport must survive panning and clipping.
context.fixture = [...context.fixture.slice(0, 9),
 {seq:10,time:310,type:'state',node:'node-2',state:{role:'candidate',term:4,votedFor:'node-2'}},
 {seq:11,time:320,type:'send',id:'heartbeat',from:'node-1',to:'node-2',payload:{type:'Heartbeat',term:4}},
 {seq:12,time:400,type:'receive',id:'heartbeat',from:'node-1',to:'node-2'},
 {seq:13,time:450,type:'send',id:'long',from:'node-2',to:'node-3',payload:{type:'Slow',value:42}},
 {seq:14,time:5000,type:'receive',id:'long',from:'node-2',to:'node-3'},
 {seq:15,time:6000,type:'lifecycle',action:'stop'}
];
vm.runInContext('events=[]; ends.clear(); sends.clear(); resetCache(); latestTime=0; live=false; fixture.forEach(receiveEvent); seek(events.length); switchView("spacetime"); panSpace(2000)', context);
assert.equal(document.querySelector('[data-message-id="long"]').dataset.status, 'received', 'long message crossing viewport remains visible');
assert.equal(document.querySelector('#event-count').textContent, '15', 'pan preserves replay position');
assert.equal(document.querySelector('#spacetime-follow').getAttribute('aria-pressed'), 'false');
vm.runInContext('panSpace(0)', context);
assert.equal(document.querySelectorAll('.time-state').length, 3, 'duplicate state report is omitted from chart only');
assert.equal(vm.runInContext('events.length', context), 15, 'chart does not remove recorded events');
const hide = document.querySelector('#spacetime-heartbeats');
hide.checked = true; hide.onchange({target:hide});
assert.equal(document.querySelector('[data-message-id="heartbeat"]'), null, 'heartbeat filter works');
assert.ok(document.querySelector('[data-message-id="m1"]'), 'other message types remain');
vm.runInContext('zoomSpace(100); followSpace()', context);
assert.equal(Number(document.querySelector('#spacetime').dataset.end) - Number(document.querySelector('#spacetime').dataset.start), 100, '100ms zoom');
assert.equal(document.querySelector('#spacetime-follow').getAttribute('aria-pressed'), 'true');
// Earlier experiment-scoped drafts must not replace shared protocol code.
store.set(`distvis-draft:${experimentId}`,JSON.stringify({revision:1,projects:{shared:{entry:'.',files:[
  {path:'main.go',content:'package main\nfunc main(){println("recovered draft")}\n',encoding:'utf8'},
  {path:'go.mod',content:'module lesson/recovered\n\ngo 1.24\n',encoding:'utf8'}
]}}}));
await vm.runInContext('migrateExperimentDrafts()',context);
const recoveredId=store.get(`distvis-draft-migrated:${experimentId}`);
assert.ok(recoveredId);assert.notEqual(recoveredId,protocolId);
const recovered=await (await request('/api/protocol-projects/'+recoveredId)).json();
assert.match(Buffer.from(recovered.projects.shared.files.find(f=>f.path==='main.go').content,'base64').toString(),/recovered draft/);
const parentAfter=await (await request('/api/protocol-projects/'+protocolId)).json();
assert.match(Buffer.from(parentAfter.projects.shared.files.find(f=>f.path==='protocol.go').content,'base64').toString(),/文件切换保留修改/);
const beforeCount=(await (await request('/api/protocol-projects')).json()).length;
await vm.runInContext('migrateExperimentDrafts()',context);
assert.equal((await (await request('/api/protocol-projects')).json()).length,beforeCount);
assert.ok(store.get(`distvis-draft:${experimentId}`),'original draft is retained');
// Archiving is reversible and does not discard a pending code edit.
await vm.runInContext(`openProtocol('${protocolId}', 'code')`,context);
editor.value+='\n// preserved across archive\n';editor.oninput({target:editor});
await click('#archive-workspace');
assert.equal(document.querySelector('#archive-workspace').textContent,'恢复协议');
assert.match(document.querySelector('#source-editor').value,/preserved across archive/);
await click('#nav-library');
assert.equal(document.querySelector(`#experiment-cards [data-protocol-project="${protocolId}"]`),null);
assert.ok(document.querySelector(`#imported-cards [data-protocol-project="${protocolId}"]`));
await vm.runInContext(`openProtocol('${protocolId}', 'code')`,context);
await click('#archive-workspace');
assert.equal(document.querySelector('#archive-workspace').textContent,'归档协议');
assert.match(document.querySelector('#source-editor').value,/preserved across archive/);
console.log('PASS: DOM interactions, slow clock, message/node details, causal replay isolation, viewport-crossing messages, state deduplication, heartbeat filtering and zoom. No actual browser rendering was tested.');
process.exit(0);
