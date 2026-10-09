import { test } from 'node:test';
import assert from 'node:assert/strict';
import { mkdtempSync, mkdirSync, writeFileSync, readFileSync, symlinkSync, realpathSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { ProtocolLibrary, snapshotProject, snapshotWorkspace, k8sHostPath } from '../server/library.js';
import { projectDockerfile, compileProject, buildImages } from '../server/build.js';
import { bindMount } from '../server/runtime.js';

test('directory discovery, persisted root and faithful multi-file snapshots', () => {
  const data=mkdtempSync(join(tmpdir(),'distvis-library-'));
  const root=join(data,'protocols');mkdirSync(root);
  const project=join(root,'demo');mkdirSync(join(project,'api'),{recursive:true});
  writeFileSync(join(project,'go.mod'),'module demo\n\ngo 1.24\n');
  writeFileSync(join(project,'main.go'),'package main\nfunc main(){}\n');
  writeFileSync(join(project,'api','request.proto'),'syntax = "proto3";');
  writeFileSync(join(project,'asset.bin'),Buffer.from([0,255,1]));
  const library=new ProtocolLibrary(data,root);
  let result=library.discover();
  assert.equal(result.entries.length,1);
  assert.equal(result.entries[0].entry,'.');
  const snapshot=snapshotProject(library.select('directory:demo'),join(data,'snapshot'));
  assert.equal(snapshot.files.length,4);
  assert.deepEqual(readFileSync(join(data,'snapshot','asset.bin')),Buffer.from([0,255,1]));
  writeFileSync(join(project,'main.go'),'package main\nfunc main(){println("new")}\n');
  assert.notEqual(snapshotProject(library.select('directory:demo'),join(data,'new')).sha256,snapshot.sha256);
  assert.match(readFileSync(join(data,'snapshot','main.go'),'utf8'),/main\(\)\{\}/);
  mkdirSync(join(root,'broken'));result=library.discover();
  assert.match(result.errors[0],/缺少 go.mod/);
  library.setRoot(root);
  assert.equal(new ProtocolLibrary(data,'/missing').root,realpathSync(root));
  assert.throws(()=>library.select('directory:../../outside'));
  symlinkSync('/etc/passwd',join(project,'escape'));
  assert.throws(()=>snapshotProject(library.select('directory:demo'),join(data,'bad')),/符号链接/);
});
test('optional entry manifest cannot inject a build command or escape the project', () => {
  const data=mkdtempSync(join(tmpdir(),'distvis-entry-')),root=join(data,'library');
  mkdirSync(join(root,'demo'),{recursive:true});
  writeFileSync(join(root,'demo','go.mod'),'module demo\n');
  const library=new ProtocolLibrary(data,root);
  for(const entry of ['../outside','./x; touch /bad','$(env)','/tmp']){
    writeFileSync(join(root,'demo','distvis.json'),JSON.stringify({entry}));
    assert.equal(library.discover().entries.length,0);
  }
});
test('K8s paths address the Docker Desktop VM and compiler reads a mounted snapshot', () => {
  assert.equal(k8sHostPath('/Users/test/protocols',{},'darwin'),'/host_mnt/Users/test/protocols');
  assert.equal(k8sHostPath('/opt/protocols',{},'linux'),'/opt/protocols');
  assert.equal(k8sHostPath('/Users/test',{DISTVIS_K8S_HOST_PREFIX:'/custom'},'darwin'),'/custom/Users/test');
  assert.match(compileProject,/cp -R \/protocol\/\./);
  assert.match(compileProject,/go mod edit -replace=distvis=\/opt\/distvis/);
  assert.match(compileProject,/go build .*"\$DISTVIS_ENTRY"/);
  const file=projectDockerfile(buildImages({}));
  assert.match(file,/COPY sdk/);
  assert.doesNotMatch(file,/COPY project/,'user code is mounted at startup');
  assert.equal(bindMount('/Users/test/含中文, directory','/protocols'),'type=bind,"src=/Users/test/含中文, directory",dst=/protocols,readonly');
});

test('editor workspaces preserve multi-file bytes and reject unsafe paths before writing', () => {
  const data=mkdtempSync(join(tmpdir(),'distvis-workspace-'));
  const draft={entry:'./cmd/node',files:[
    {path:'go.mod',content:'module lesson/custom\n\ngo 1.24\n'},
    {path:'cmd/node/main.go',content:'package main\nfunc main() {}\n'},
    {path:'api/types.go',content:'package api\n// 中文 payload\n'},
    {path:'asset.bin',content:'AP8=',encoding:'base64'},
  ]};
  const archive=snapshotWorkspace(draft,join(data,'snapshot'));
  assert.equal(archive.entry,'./cmd/node');
  assert.deepEqual(readFileSync(join(data,'snapshot','asset.bin')),Buffer.from([0,255]));
  assert.equal(readFileSync(join(data,'snapshot','api/types.go'),'utf8'),draft.files[2].content);
  assert.equal(snapshotWorkspace({...archive,files:archive.files.map(f=>({...f,encoding:'base64'}))}).sha256,archive.sha256);
  for(const path of ['../escape','/tmp/escape','api/../../escape','api\\escape','.git/config','api//x','go.mod']){
    assert.throws(()=>snapshotWorkspace({...draft,files:[...draft.files,{path,content:'x'}]}),/路径/);
  }
  assert.throws(()=>snapshotWorkspace({...draft,files:[...draft.files,{path:'cmd',content:'x'}]}),/冲突/);
  assert.throws(()=>snapshotWorkspace({...draft,entry:'./cmd/node;echo bad'}),/入口/);
  assert.throws(()=>snapshotWorkspace({...draft,entry:'./missing'}),/package main/);
  assert.throws(()=>snapshotWorkspace({...draft,files:draft.files.slice(1)}),/go.mod/);
  assert.throws(()=>snapshotWorkspace({...draft,files:[...draft.files,{path:'bad.bin',content:'??',encoding:'base64'}]}),/base64/);
  assert.equal(snapshotProject({directory:join(data,'snapshot')}).sha256,archive.sha256);
});

test('Go header metadata replaces JSON for discovery and editor archives',()=>{
  const data=mkdtempSync(join(tmpdir(),'distvis-comments-')),root=join(data,'protocols'),dir=join(root,'demo');
  mkdirSync(join(dir,'cmd/peer'),{recursive:true});
  const code='// distvis:name 注释协议\n// distvis:description 任意节点提交数字。\npackage main\nfunc main(){}\n';
  writeFileSync(join(dir,'go.mod'),'module demo\n\ngo 1.24\n');
  writeFileSync(join(dir,'cmd/peer/main.go'),code);
  const library=new ProtocolLibrary(data,root);
  const item=library.select('directory:demo');
  assert.equal(item.entry,'./cmd/peer');assert.equal(item.name,'注释协议');assert.equal(item.description,'任意节点提交数字。');
  const draft={files:[{path:'go.mod',content:'module demo\n'},{path:'cmd/peer/main.go',content:code}]};
  const archive=snapshotWorkspace(draft);
  assert.equal(archive.entry,item.entry);assert.equal(archive.name,item.name);assert.equal(archive.description,item.description);
  assert.equal(archive.files.some(f=>f.path==='distvis.json'),false);
  writeFileSync(join(dir,'distvis.json'),JSON.stringify({name:'Legacy',description:'Legacy description',entry:'./cmd/peer'}));
  assert.equal(library.select('directory:demo').name,'注释协议','Go annotations take precedence');
  const block='/*\n * distvis:name Block header\n * distvis:entry ./cmd/peer\n */\npackage main\n';
  const blocked=snapshotWorkspace({...draft,files:[...draft.files,{path:'doc.go',content:block.replace('distvis:name Block header','An ordinary comment')}]});
  assert.equal(blocked.entry,'./cmd/peer');
  const ordinary='package main\nfunc main(){println(`\n// distvis:entry ../../escape\n`)}';
  assert.equal(snapshotWorkspace({files:[draft.files[0],{path:'main.go',content:ordinary}]}).entry,'.','strings cannot declare metadata');
  assert.throws(()=>snapshotWorkspace({...draft,files:[...draft.files,{path:'cmd/peer/doc.go',content:'// distvis:name Duplicate\npackage main'}]}),/重复/);
  for(const directive of ['entry ../../escape','entry ./cmd/peer;echo bad','name '+ 'x'.repeat(101),'description '+ 'x'.repeat(1001),'unknown foo']){
    assert.throws(()=>snapshotWorkspace({files:[draft.files[0],{path:'main.go',content:'// distvis:'+directive+'\npackage main\nfunc main(){}'}]}));
  }
  const multiple={files:[draft.files[0],{path:'cmd/a/main.go',content:'package main\nfunc main(){}'},{path:'cmd/b/main.go',content:'package main\nfunc main(){}'}]};
  assert.throws(()=>snapshotWorkspace(multiple),/唯一 main/);
  assert.equal(snapshotWorkspace({...multiple,entry:'./cmd/b'}).entry,'./cmd/b');
});
