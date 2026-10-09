import test from 'node:test';
import assert from 'node:assert/strict';
import {mkdtemp,readFile,writeFile,mkdir,readdir,realpath} from 'node:fs/promises';
import {tmpdir} from 'node:os';
import {join} from 'node:path';
import {fileURLToPath} from 'node:url';
import {Client} from '@modelcontextprotocol/sdk/client/index.js';
import {StdioClientTransport} from '@modelcontextprotocol/sdk/client/stdio.js';

// Exercise actual stdio MCP negotiation and tools against an isolated coordinator.
test('agent creates a directory protocol, imports safely, resyncs by revision and scopes experiments',async()=>{
  const work=await mkdtemp(join(tmpdir(),'distvis-mcp-'));
  process.env.DISTVIS_DATA=join(work,'data');process.env.DISTVIS_PROTOCOL_DIR=join(work,'protocols');
  await mkdir(process.env.DISTVIS_PROTOCOL_DIR);
  const {server}=await import('../server/index.js');
  await new Promise(resolve=>server.listen(0,'127.0.0.1',resolve));
  const base=`http://127.0.0.1:${server.address().port}`;
  const transport=new StdioClientTransport({command:process.execPath,args:[fileURLToPath(new URL('../mcp/server.js',import.meta.url))],env:{PATH:process.env.PATH,DISTVIS_URL:base},stderr:'pipe'});
  const client=new Client({name:'test',version:'1.0.0'});
  const call=(name,args={})=>client.callTool({name,arguments:args});
  function data(result){assert.ok(!result.isError,JSON.stringify(result));return result.structuredContent;}
  try{
    await client.connect(transport);
    assert.equal((await client.listTools()).tools.length,12);
    const resources=await client.listResources();assert.equal(resources.resources.length,3);
    assert.match((await client.readResource({uri:'distvis://docs/go'})).contents[0].text,/Runtime 生命周期/);
    assert.ok((await client.getPrompt({name:'create_protocol',arguments:{description:'实现互斥算法'}})).messages[0].content.text.includes('实现互斥算法'));
    const catalog=data(await call('distvis_catalog'));assert.equal(catalog.library.root,process.env.DISTVIS_PROTOCOL_DIR);
    const generated=data(await call('distvis_scaffold_protocol',{folder:'new-ring',name:'New $& Ring',description:'RPC scaffold'}));
    assert.equal(generated.directory,await realpath(join(process.env.DISTVIS_PROTOCOL_DIR,'new-ring')));
    const file=join(generated.directory,'main.go'),original=await readFile(file,'utf8');assert.ok(original.includes('// distvis:name New $& Ring'));
    assert.ok((await call('distvis_scaffold_protocol',{folder:'new-ring',name:'Overwrite'})).isError);
    assert.ok((await call('distvis_scaffold_protocol',{folder:'../escape',name:'Escape'})).isError);
    assert.ok((await call('distvis_scaffold_protocol',{folder:'bad',name:'Bad\npackage bad'})).isError);
    assert.equal(await readFile(file,'utf8'),original);
    const imported=data(await call('distvis_import_protocol',{directoryId:generated.directoryId,acceptance:true}));
    const initial=data(await call('distvis_get_protocol',{protocolId:imported.id}));
    assert.equal(initial.protocol.archived,true);assert.equal(initial.protocol.revision,1);
    assert.ok((await call('distvis_import_protocol',{directoryId:generated.directoryId})).isError,'same directory must not silently duplicate');
    const draft=original+'\n// changed in the source directory\n';await writeFile(file,draft);
    const before=data(await call('distvis_get_protocol',{protocolId:imported.id}));
    assert.equal(Buffer.from(before.protocol.projects.shared.files.find(f=>f.path==='main.go').content,'base64').toString(),original,'source edits are not implicitly synced');
    assert.ok((await call('distvis_import_protocol',{directoryId:generated.directoryId,protocolId:imported.id})).isError);
    const update=data(await call('distvis_import_protocol',{directoryId:generated.directoryId,protocolId:imported.id,revision:1}));
    assert.equal(update.revision,2);
    assert.ok((await call('distvis_import_protocol',{directoryId:generated.directoryId,protocolId:imported.id,revision:1})).isError,'stale revisions must fail');
    assert.equal((await readdir(process.env.DISTVIS_PROTOCOL_DIR)).length,1);
    const experiment=data(await call('distvis_create_experiment',{protocolId:imported.id,name:'Slow network',nodeCount:4,latency:500}));
    assert.equal(experiment.protocolId,imported.id);assert.equal(experiment.archived,true);assert.equal(experiment.settings.runtime,'docker');
    assert.ok((await call('distvis_inspect_run',{runId:experiment.id})).isError,'HTTP failures surface as MCP errors');
    assert.ok((await call('distvis_fault',{runId:experiment.id,fault:{kind:'link',from:'node-1',to:'node-2'}})).isError,'incomplete link faults fail validation');
    assert.equal(data(await call('distvis_get_protocol',{protocolId:imported.id})).experiments.length,1);
  }finally{await client.close();await new Promise(resolve=>server.close(resolve));}
});
