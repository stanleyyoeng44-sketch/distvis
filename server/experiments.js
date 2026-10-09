import { mkdirSync, readFileSync, writeFileSync, readdirSync, renameSync } from 'node:fs';
import { join } from 'node:path';
import { randomUUID, createHash } from 'node:crypto';
import { snapshotWorkspace } from './library.js';
import { validateConfig } from './engine.js';

export function archiveDraft(project) {
  return {...project, files:project.files.map(f=>({...f,encoding:f.encoding || project.encoding || 'utf8'}))};
}
export function codeFingerprint(projects) {
  return createHash('sha256').update(JSON.stringify(Object.entries(projects).sort(([a],[b])=>a.localeCompare(b)).map(([node,p])=>{
    const normalized=snapshotWorkspace(archiveDraft(p));
    return [node,normalized.entry,normalized.sha256];
  }))).digest('hex');
}
function validateProjects(projects) {
  if (!projects || typeof projects !== 'object' || Array.isArray(projects) || !projects.shared) throw new Error('实验需要默认代码项目');
  return Object.fromEntries(Object.entries(projects).map(([node,project])=>{
    if (node !== 'shared' && !/^node-([1-9]|1[0-2])$/.test(node)) throw new Error('节点项目名称不合法');
    return [node,snapshotWorkspace(archiveDraft(project))];
  }));
}
function settings(input = {}, protocol) {
  const config=validateConfig({...input,protocol:['raft','token','gossip'].includes(protocol)?protocol:'custom'});
  return Object.fromEntries(['runtime','nodeCount','seed','latency','bandwidth','delayModel','jitter','parameters','limits','network','networkProfiles'].filter(k=>config[k]!==undefined).map(k=>[k,config[k]]));
}
function legacySource(source) {
  return {entry:'.',files:[
    {path:'main.go',content:source},
    {path:'go.mod',content:'module lesson/imported\n\ngo 1.24\n\nrequire distvis v0.0.0\n'},
  ]};
}

// Protocols own Go projects; experiments own settings; runs own immutable evidence.
export class ExperimentStore {
  constructor(dataDir) {
    this.directory=join(dataDir,'experiments');
    this.items=new Map();
    this.protocolDirectory=join(dataDir,'protocols');
    this.protocolItems=new Map();
    mkdirSync(this.protocolDirectory,{recursive:true});
    for(const file of readdirSync(this.protocolDirectory)){if(file.endsWith('.json')){const p=JSON.parse(readFileSync(join(this.protocolDirectory,file),'utf8'));this.protocolItems.set(p.id,p);}}
    mkdirSync(this.directory,{recursive:true});
    for(const file of readdirSync(this.directory)){
      if(!file.endsWith('.json'))continue;
      const item=JSON.parse(readFileSync(join(this.directory,file),'utf8'));
      if(!item.protocolId){
        // Only identical code from the same source can share a migrated protocol.
        const fingerprint=createHash('sha256').update(JSON.stringify([item.protocol,item.origin?.root,item.origin?.directory,Object.entries(item.projects).sort().map(([node,p])=>[node,p.entry || '.',p.sha256 || snapshotWorkspace(archiveDraft(p)).sha256])])).digest('hex');
        let parent=[...this.protocolItems.values()].find(p=>p.migrationKey===fingerprint);
        if(!parent)parent=this.createProtocol({name:item.projects.shared.name || item.name,protocol:item.protocol,projects:item.projects,origin:item.origin,imported:item.imported,migrationKey:fingerprint});
        item.protocolId=parent.id;delete item.projects;this.write(item);
      }
      this.items.set(item.id,item);
    }
  }
  get(id) {
    const item=this.items.get(id);
    if(!item)throw new Error('实验工作区不存在');
    return item;
  }
  write(item) {
    const path=join(this.directory,`${item.id}.json`), temporary=path+'.tmp';
    writeFileSync(temporary,JSON.stringify(item,null,2));
    renameSync(temporary,path);
    this.items.set(item.id,item);
    return item;
  }
  create({name,protocol,protocolId,projects,settings:config,imported=false,origin,archived=false}, id=randomUUID()) {
    if(typeof name!=='string'||!name.trim()||name.length>100)throw new Error('实验名称应为 1–100 个字符');
    if(protocol!==undefined && (typeof protocol!=='string' || !/^(?:raft|token|gossip|grpc|custom|directory:[^/\\]+)$/.test(protocol)))throw new Error('未知实验协议来源');
    if(protocolId && projects)throw new Error('实验共用协议代码，不能携带独立代码');
    if(protocolId)this.getProtocol(protocolId);
    const configProtocol=protocolId?this.getProtocol(protocolId).protocol:protocol;
    const checkedSettings=settings(config,configProtocol);
    const parent=protocolId?this.getProtocol(protocolId):this.createProtocol({name,protocol,projects,origin,imported,archived});
    protocol=parent.protocol;
    const now=new Date().toISOString();
    return this.write({id,name:name.trim(),protocol,protocolId:parent.id,
      settings:checkedSettings,revision:1,createdAt:now,updatedAt:now,imported,archived:!!(archived || parent.archived),...(origin?{origin}:{})});
  }
  save(id,input) {
    const before=this.items.get(id);
    if(!before)throw new Error('实验不存在');
    if(input.projects)throw new Error('代码属于协议，请在协议代码页保存');
    if(input.archived!==undefined && typeof input.archived!=='boolean')throw new Error('归档状态必须是布尔值');
    if(input.revision!==before.revision)throw new Error('实验已在其他页面更新，请重新打开后再保存；本地草稿仍保留');
    const name=input.name ?? before.name;
    if(typeof name!=='string'||!name.trim()||name.length>100)throw new Error('实验名称应为 1–100 个字符');
    return this.write({...before,name:name.trim(),archived:input.archived ?? before.archived,

      settings:input.settings?settings(input.settings,before.protocol):before.settings,
      revision:before.revision+1,updatedAt:new Date().toISOString()});
  }
  getProtocol(id) {
    const seen=new Set();
    let p=this.protocolItems.get(id);
    while(p?.mergedInto){
      if(seen.has(p.id))throw new Error('协议合并引用异常');
      seen.add(p.id);p=this.protocolItems.get(p.mergedInto);
    }
    if(!p)throw new Error('协议不存在');return p;
  }
  writeProtocol(p) {
    const path=join(this.protocolDirectory,`${p.id}.json`);
    writeFileSync(path+'.tmp',JSON.stringify(p,null,2));renameSync(path+'.tmp',path);
    this.protocolItems.set(p.id,p);return p;
  }
  createProtocol({name,protocol='custom',projects,origin,imported=false,migrationKey,archived=false}) {
    if(typeof name!=='string'||!name.trim()||name.length>100)throw new Error('协议名称应为 1–100 个字符');
    if(typeof protocol!=='string'||!/^(?:raft|token|gossip|grpc|custom|directory:[^/\\]+)$/.test(protocol))throw new Error('未知协议来源');
    const now=new Date().toISOString();
    return this.writeProtocol({id:randomUUID(),name:name.trim(),protocol,projects:validateProjects(projects),revision:1,createdAt:now,updatedAt:now,origin,imported,migrationKey,archived:!!archived});
  }
  saveProtocol(id,input) {
    const p=this.getProtocol(id);
    if(input.archived!==undefined && typeof input.archived!=='boolean')throw new Error('归档状态必须是布尔值');
    if(input.revision!==p.revision)throw new Error('协议已在其他页面更新，请重新打开；本地草稿仍保留');
    const name=input.name ?? p.name;
    if(typeof name!=='string'||!name.trim()||name.length>100)throw new Error('协议名称应为 1–100 个字符');
    return this.writeProtocol({...p,name:name.trim(),archived:input.archived ?? p.archived,projects:input.projects?validateProjects(input.projects):p.projects,revision:p.revision+1,updatedAt:new Date().toISOString()});
  }
  // Explicit maintenance only. Names alone never trigger merging.
  mergeProtocol(sourceId,targetId) {
    const source=this.getProtocol(sourceId),target=this.getProtocol(targetId);
    if(source.id===target.id)return target;
    if(codeFingerprint(source.projects)!==codeFingerprint(target.projects))throw new Error('协议代码或构建入口不同，不能合并');
    for(const child of this.items.values())if(child.protocolId===source.id)this.write({...child,protocolId:target.id,origin:child.origin || source.origin,revision:child.revision+1});
    // Keep the old record and ID for archived links and rollback.
    this.writeProtocol({...source,mergedInto:target.id});
    return target;
  }
  listProtocols(runs) {
    const all=this.list(runs);
    return [...this.protocolItems.values()].filter(p=>!p.mergedInto).map(({projects,...p})=>{
      const children=all.filter(e=>e.protocolId===p.id);
      const mergedIds=[...this.protocolItems.values()].filter(old=>old.mergedInto && this.getProtocol(old.id).id===p.id).map(old=>old.id);
      return {...p,mergedIds,experimentCount:children.length,archivedExperimentCount:children.filter(e=>e.archived).length,runCount:children.reduce((n,e)=>n+e.runCount,0)};
    }).sort((a,b)=>b.updatedAt.localeCompare(a.updatedAt));
  }
  list(runs) {
    return [...this.items.values()].map(({projects,...item})=>{
      const history=[...runs.values()].filter(r=>r.config.experimentId===item.id).sort((a,b)=>b.createdAt.localeCompare(a.createdAt));
      const latest=history[0];
      return {...item,runCount:history.length,latestRun:latest?{id:latest.id,status:latest.status,createdAt:latest.createdAt}:null};
    }).sort((a,b)=>b.updatedAt.localeCompare(a.updatedAt));
  }
  // Old runs had no experiment identity. Group known examples/directories by
  // origin; keep each custom run separate so unrelated student code never merges.
  adopt(run,source,template) {
    if(run.config.experimentId && this.items.has(run.config.experimentId))return false;
    const protocol=run.config.protocol;
    const key=['raft','token','gossip'].includes(protocol)?`example:${protocol}`:
      protocol.startsWith('directory:')?`directory:${run.config.project?.root || ''}:${protocol}`:`run:${run.id}`;
    const hash=createHash('sha256').update(key).digest('hex');
    const id=[hash.slice(0,8),hash.slice(8,12),hash.slice(12,16),hash.slice(16,20),hash.slice(20,32)].join('-');
    if(!this.items.has(id)){
      const project=template || source.project || legacySource(source.source);
      const projects={shared:project,...Object.fromEntries(Object.entries(source.nodeSources || {}).map(([node,text])=>[node,legacySource(text)])),...source.nodeProjects};
      this.create({name:template?.name || source.project?.name || run.config.name,protocol,projects,settings:run.config,imported:true},id);
    }
    run.config.experimentId=id;
    return true;
  }
}
