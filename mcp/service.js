import {readFile, mkdir, writeFile, unlink, rmdir, realpath} from 'node:fs/promises';
import {join} from 'node:path';
import {fileURLToPath} from 'node:url';

const root=fileURLToPath(new URL('../',import.meta.url));
export const documents={go:'docs/GO-API.md',platform:'docs/PLATFORM-API.md',agent:'skills/distvis-protocol/SKILL.md'};
export function readGuide(topic){return readFile(join(root,documents[topic]),'utf8');}
export function createService(base=process.env.DISTVIS_URL || 'http://localhost:3000'){
  base=new URL(base).origin;
  async function api(path,body,acceptance=false){
    const response=await fetch(base+path,{signal:AbortSignal.timeout(30000),...(body===undefined?{}:{method:'POST',headers:{'Content-Type':'application/json',...(acceptance?{'X-Distvis-Purpose':'acceptance'}:{})},body:JSON.stringify(body)})});
    const data=await response.json();
    if(!response.ok)throw new Error(`HTTP ${response.status}: ${data.error || response.statusText}`);
    return data;
  }
  return {
    async catalog(){
      const [library,protocols,runs]=await Promise.all([api('/api/library'),api('/api/protocol-projects'),api('/api/runs')]);
      return {base,library,protocols,activeRuns:runs.filter(r=>['starting','running'].includes(r.status))};
    },
    async scaffold({folder,name,description='在此实现协议算法；初始程序演示普通 RPC 请求、回复和状态上报。'}){
      const library=await api('/api/library');
      const parent=await realpath(library.root),directory=join(parent,folder);
      const main=(await readFile(join(root,'docs/examples/ping/main.go'),'utf8')).replace('// distvis:name RPC Ping',()=>`// distvis:name ${name}`).replace(/^\/\/ distvis:description .+$/m,()=>`// distvis:description ${description}`);
      const mod=`module lesson/${folder}\n\ngo 1.24\n\nrequire distvis v0.0.0\n`;
      // mkdir is exclusive: an existing user directory is never overwritten.
      await mkdir(directory);
      const created=[];
      try{
        for(const [file,content] of [['go.mod',mod],['main.go',main]]){
          const path=join(directory,file);await writeFile(path,content,{flag:'wx'});created.push(path);
        }
      }catch(error){
        for(const path of created)await unlink(path).catch(()=>{});
        await rmdir(directory).catch(()=>{});throw error;
      }
      return {directory,directoryId:`directory:${encodeURIComponent(folder)}`,files:['go.mod','main.go'],next:'用文件编辑工具实现用户要求的算法，再调用 distvis_import_protocol。当前骨架仅为 RPC Ping，不是用户协议的完成实现。'};
    },
    async importProtocol({directoryId,protocolId,revision,name,acceptance=false,createCopy=false}){
      if(protocolId && revision===undefined)throw new Error('更新协议必须提供读取到的 revision。');
      if(!protocolId && revision!==undefined)throw new Error('revision 只能用于更新已有 protocolId。');
      const project=await api('/api/project?protocol='+encodeURIComponent(directoryId));
      let item;
      if(protocolId){
        const before=await api(`/api/protocol-projects/${protocolId}`);
        item=await api(`/api/protocol-projects/${protocolId}`,{revision,...(name?{name}:{}),projects:{...before.projects,shared:project}});
      }else{
        const matches=(await api('/api/protocol-projects')).filter(p=>p.origin?.directory===project.directory);
        if(matches.length && !createCopy)throw new Error(`此目录已有协议 ${matches.map(p=>`${p.id} (revision ${p.revision})`).join(', ')}。读取后用 protocolId + revision 更新；只有明确需要独立副本才设置 createCopy。`);
        item=await api('/api/protocol-projects',{name:name || project.name,protocol:directoryId},acceptance);
      }
      return {id:item.id,name:item.name,revision:item.revision,projects:Object.fromEntries(Object.entries(item.projects).map(([k,p])=>[k,{entry:p.entry,sha256:p.sha256,fileCount:p.files.length}])),url:`${base}/#protocol/${item.id}/code`,note:'平台保存源码快照。以后修改目录文件后须重新同步；保留已有节点专用代码副本。'};
    },
    async getProtocol({protocolId}){
      const [protocol,experiments]=await Promise.all([api(`/api/protocol-projects/${protocolId}`),api(`/api/protocol-projects/${protocolId}/experiments`)]);
      return {protocol,experiments};
    },
    async createExperiment({protocolId,name,runtime='docker',nodeCount=3,latency=80,bandwidth=128,delayModel='fixed',jitter=0,seed=42,acceptance=true}){
      return api('/api/experiments',{protocolId,name,settings:{runtime,nodeCount,latency,bandwidth,delayModel,jitter,seed}},acceptance);
    },
    async startRun({experimentId,name}){
      const r=await api('/api/runs',{experimentId,...(name?{name}:{})});
      return {...r,url:`${base}/#experiment/${r.config.experimentId}/visual`,next:'异步启动：用 distvis_inspect_run 观察状态和构建错误，等待目标节点 input_schema 后再输入。'};
    },
    async inspectRun({runId,after=0,limit=100,types}){
      const {events,...run}=await api(`/api/runs/${runId}`),nodes={};
      for(const e of events){
        if(e.node){const n=nodes[e.node] ||= {};if(e.type==='state')n.state=e.state;if(e.type==='input_schema')n.inputs=e.schema;if(e.type==='node')n.online=e.online;}
        if(e.type==='lifecycle' && e.action==='start')for(const id of e.nodes || [])(nodes[id] ||= {}).online=true;
      }
      const remaining=events.filter(e=>e.seq>after && (!types || types.includes(e.type))),page=remaining.slice(0,limit),hasMore=remaining.length>page.length;
      return {...run,nodes,events:page,hasMore,nextAfter:hasMore?page.at(-1).seq:Math.max(after,events.at(-1)?.seq || 0),exportUrl:`${base}/api/runs/${runId}/export`};
    },
    async input({runId,node,action,values}){return api(`/api/runs/${runId}/commands`,{node,action,values});},
    async inputConcurrent({runId,inputs}){return api(`/api/runs/${runId}/commands`,{batch:inputs});},
    async fault({runId,fault}){return api(`/api/runs/${runId}/faults`,fault);},
    async stopRun({runId}){return api(`/api/runs/${runId}/stop`,{});},
  };
}
