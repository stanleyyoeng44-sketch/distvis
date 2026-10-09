// Offline, explicit maintenance; never infer merges or archives from display names.
// node scripts/organize-library.mjs plan.json [--apply]
import {readFileSync,writeFileSync,mkdirSync,cpSync} from 'node:fs';
import {resolve,join} from 'node:path';
import {ExperimentStore,codeFingerprint} from '../server/experiments.js';
const plan=JSON.parse(readFileSync(process.argv[2],'utf8'));
const data=resolve(process.env.DISTVIS_DATA || 'data');
const store=new ExperimentStore(data);
for(const [from,to] of plan.merges || []){
  if(codeFingerprint(store.getProtocol(from).projects)!==codeFingerprint(store.getProtocol(to).projects))throw new Error(`Different code: ${from} / ${to}`);
}
for(const id of plan.archiveProtocols || [])store.getProtocol(id);
for(const id of plan.archiveExperiments || [])store.get(id);
for(const [id,name] of Object.entries(plan.names || {})){
  store.getProtocol(id);if(!name.trim() || name.length>100)throw new Error('Invalid name');
}
console.log(JSON.stringify(plan,null,2));
if(process.argv.includes('--apply')){
  const backup=resolve('artifacts',`library-backup-${Date.now()}`);
  mkdirSync(backup,{recursive:true});
  for(const directory of ['protocols','experiments'])cpSync(join(data,directory),join(backup,directory),{recursive:true});
  writeFileSync(join(backup,'plan.json'),JSON.stringify(plan,null,2));
  for(const [from,to] of plan.merges || [])store.mergeProtocol(from,to);
  for(const id of plan.archiveExperiments || []){
    const e=store.get(id);if(!e.archived)store.save(id,{revision:e.revision,archived:true});
  }
  for(const id of plan.archiveProtocols || []){
    const p=store.getProtocol(id);if(!p.archived)store.saveProtocol(id,{revision:p.revision,archived:true});
  }
  for(const [id,name] of Object.entries(plan.names || {})){
    const p=store.getProtocol(id);if(p.name!==name)store.saveProtocol(id,{revision:p.revision,name});
  }
  console.log(`Applied. Reversible backup: ${backup}`);
}else console.log('Preview only. Stop the coordinator before applying.');
