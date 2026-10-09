import assert from 'node:assert/strict';
import {writeFile} from 'node:fs/promises';
const {chromium}=await import(process.env.DISTVIS_PLAYWRIGHT_MODULE || 'playwright');
const base=process.env.DISTVIS_URL || 'http://localhost:3000', runtime=process.env.DISTVIS_RUNTIME || 'docker';
async function api(path,data){const r=await fetch(base+path,data===undefined?{}:{method:'POST',headers:{'Content-Type':'application/json','X-Distvis-Purpose':'acceptance'},body:JSON.stringify(data)});const body=await r.json();assert.ok(r.ok,JSON.stringify(body));return body;}
async function wait(check,label,ms=30000){const end=Date.now()+ms;while(Date.now()<end){const value=await check();if(value)return value;await new Promise(r=>setTimeout(r,250));}throw new Error('timeout '+label);}
assert.ok(!(await api('/api/runs')).some(r=>['running','starting'].includes(r.status)));
const browser=await chromium.launch({headless:true,...(process.env.DISTVIS_BROWSER_PATH?{executablePath:process.env.DISTVIS_BROWSER_PATH}:{})});
const page=await browser.newPage({viewport:{width:1440,height:1100}}),errors=[],runIds=[];
await page.setExtraHTTPHeaders({'X-Distvis-Purpose':'acceptance'});
page.on('pageerror',e=>errors.push(e.message));
const stamp=Date.now(),name=`层级验收 · 令牌环 ${stamp}`;
let parent,a,b;
async function toParent(){await page.locator('#breadcrumb-protocol').click();await page.locator('#protocol-experiments-panel').waitFor();}
async function openChild(id){await toParent();await page.locator(`[data-experiment="${id}"]`).click();await page.locator('#visual-panel').waitFor();}
async function createChild(label,count,latency){
  await page.locator('#workspace-new-experiment').click();
  await page.locator('#new-form [name="name"]').fill(label);
  await page.locator('#new-form [name="nodeCount"]').fill(String(count));
  await page.locator('#new-form [name="latency"]').fill(String(latency));
  await page.locator('#new-form [name="runtime"]').selectOption(runtime);
  await page.locator('#run-submit').click();
  await page.locator('#breadcrumb-experiment').filter({hasText:label}).waitFor();
  const e=(await api(`/api/protocol-projects/${parent}/experiments`)).find(e=>e.name===label);
  assert.equal(e.protocolId,parent);assert.equal(e.runCount,0,'creation does not start nodes');return e.id;
}
async function start(id){
  // A run can be started from the visualization strip or from the run history.
  await page.locator(await page.locator('#visual-panel').isVisible()?'#new-run':'#history-run').click();
  await page.locator('#new-dialog[open]').waitFor();
  assert.equal(await page.locator('#new-form [name="protocol"]').count(),0);
  const config=(await api(`/api/experiments/${id}`)).settings;
  assert.equal(await page.locator('#new-form [name="latency"]').inputValue(),String(config.latency));
  await page.locator('#run-submit').click();
  const r=await wait(async()=>(await api(`/api/experiments/${id}/runs`)).find(r=>['starting','running'].includes(r.status)),'run created');runIds.push(r.id);
  await wait(async()=>{const run=await api(`/api/runs/${r.id}`);if(run.status==='failed')throw new Error(JSON.stringify(run.events.filter(e=>e.level==='error')));return run.status==='running'&&run.events.some(e=>e.type==='input_schema');},'real Go compilation',360000);
  return r;
}
async function submit(id,expected){const path=`/api/runs/${id}`,detail=await api(path),action=detail.events.find(e=>e.type==='input_schema'&&e.node==='node-1').schema[0].action;const cmd=await api(`${path}/commands`,{node:'node-1',action,values:{Payload:'同一协议的不同实验'}});await wait(async()=>(await api(path)).events.some(e=>e.type==='command_result'&&e.commandId===cmd.id&&e.result?.Node===expected),'shared code actually executes');}
async function stop(){await page.locator('#stop-run').click();await page.locator('#run-status').filter({hasText:'已结束'}).waitFor();await wait(async()=>!(await api('/api/runs')).some(r=>['running','starting'].includes(r.status)),'idle');}
try{
  await page.goto(base+'/#library');await page.locator('#create-experiment').click();
  await page.locator('#project-form [name="protocol"]').selectOption('token');await page.locator('#project-form [name="name"]').fill(name);await page.locator('#project-form [type="submit"]').click();
  await page.locator('#workspace-title').filter({hasText:name}).waitFor();await page.locator('#code-dialog').waitFor();
  parent=(await api('/api/protocol-projects')).find(p=>p.name===name).id;
  assert.equal(await page.locator('#experiment-tabs').isVisible(),false);assert.equal(await page.locator('#new-run').isVisible(),false);
  assert.match(await page.locator('#source-editor').inputValue(),/lab.Main/);
  await page.locator('#protocol-experiments-tab').click();
  a=await createChild('正常网络',3,80);await toParent();b=await createChild('慢速网络',4,400);
  await page.locator('#nav-history').click();assert.equal(await page.locator('.history-row').count(),0);
  await toParent();assert.equal(await page.locator('#child-experiment-cards .experiment-card').count(),2);
  await page.screenshot({path:'artifacts/protocol-experiments.png',fullPage:true});
  await page.locator('#protocol-code-tab').click();await page.locator('[data-file="protocol.go"]').click();
  const original=await page.locator('#source-editor').inputValue();
  await page.locator('#source-editor').fill(original.replace('*reply = SubmitReply{true, a.ring.id}','*reply = SubmitReply{true, "shared-v1"}'));
  await page.reload();await page.locator('#code-dialog').waitFor();await page.locator('[data-file="protocol.go"]').click();assert.match(await page.locator('#source-editor').inputValue(),/shared-v1/);
  await page.locator('#save-code').click();await page.waitForFunction(()=>document.querySelector('#workspace-save-state').textContent==='已保存');
  await openChild(a);const first=await start(a);await submit(first.id,'shared-v1');await stop();
  const oldArchive=await api(`/api/runs/${first.id}/source`);
  await page.locator('#nav-history').click();assert.equal(await page.locator('.history-row').count(),1);
  await openChild(b);await page.locator('#nav-history').click();assert.equal(await page.locator('.history-row').count(),0);
  const second=await start(b);assert.equal(second.config.nodeCount,4);assert.equal(second.config.latency,400);await submit(second.id,'shared-v1');
  await api(`/api/runs/${second.id}/faults`,{kind:'link',from:'node-1',to:'node-2',blocked:true,bidirectional:true,latency:400,bandwidth:128});
  await stop();assert.ok((await api(`/api/runs/${second.id}`)).events.some(e=>e.type==='fault'));
  assert.ok(!(await api(`/api/runs/${first.id}`)).events.some(e=>e.type==='fault'));
  await toParent();await page.locator('#protocol-code-tab').click();await page.locator('[data-file="protocol.go"]').click();
  await page.locator('#source-editor').fill((await page.locator('#source-editor').inputValue()).replace('shared-v1','shared-v2'));
  await page.locator('#save-code').click();await page.waitForFunction(()=>document.querySelector('#workspace-save-state').textContent==='已保存');
  await page.locator('[data-file="main.go"]').click();await page.screenshot({path:'artifacts/protocol-code.png',fullPage:true});
  await openChild(a);const third=await start(a);await submit(third.id,'shared-v2');await stop();
  assert.equal((await api(`/api/runs/${first.id}/source`)).project.sha256,oldArchive.project.sha256);
  assert.notEqual((await api(`/api/runs/${third.id}/source`)).project.sha256,oldArchive.project.sha256);
  assert.equal((await api(`/api/experiments/${b}`)).settings.latency,400);
  await page.locator('#nav-history').click();assert.equal(await page.locator('.history-row').count(),2);
  await page.locator(`[data-run-info="${first.id}"] > summary`).click();
  await page.locator(`[data-source-run="${first.id}"]`).click();await page.locator('#source-editor[readonly]').waitFor();await page.locator('[data-file="protocol.go"]').click();assert.match(await page.locator('#source-editor').inputValue(),/shared-v1/);
  await page.locator('#return-code-draft').click();await page.locator('#protocol-tabs').waitFor();await page.locator('[data-file="protocol.go"]').click();assert.match(await page.locator('#source-editor').inputValue(),/shared-v2/);
  const foreign=await api('/api/protocol-projects',{name:`独立协议 ${stamp}`,protocol:'token'});assert.doesNotMatch(Buffer.from(foreign.projects.shared.files.find(f=>f.path==='protocol.go').content,'base64').toString(),/shared-v2/);
  await toParent();await page.setViewportSize({width:390,height:844});assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth),true);await page.screenshot({path:'artifacts/protocol-mobile.png',fullPage:true});
  await page.setViewportSize({width:1440,height:1100});await page.locator('#nav-library').click();await page.locator('#experiment-search').fill(name);await page.screenshot({path:'artifacts/protocol-library.png',fullPage:true});
  assert.deepEqual(errors,[]);
  await writeFile(`artifacts/protocol-hierarchy-${runtime}.json`,JSON.stringify({checkedAt:new Date().toISOString(),parent,experiments:[a,b],runs:runIds,status:'passed'},null,2));
  console.log('PASS: protocol/shared Go code, sibling settings and faults, three real runs, immutable history, mobile.');
}finally{for(const id of runIds){const r=await api(`/api/runs/${id}`);if(r.status==='running')await api(`/api/runs/${id}/stop`,{});}await browser.close();}
