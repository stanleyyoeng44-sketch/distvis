// Register this checkout's MCP and skill without copying a second set of docs.
import {mkdir,lstat,realpath,symlink} from 'node:fs/promises';
import {homedir} from 'node:os';
import {dirname,join} from 'node:path';
import {fileURLToPath} from 'node:url';
import {spawnSync} from 'node:child_process';
const root=fileURLToPath(new URL('../',import.meta.url));
const source=join(root,'skills/distvis-protocol');
const target=join(process.env.CODEX_HOME || join(homedir(),'.codex'),'skills/distvis-protocol');
await mkdir(dirname(target),{recursive:true});
let existing;
try{existing=await lstat(target);}catch(error){if(error.code!=='ENOENT')throw error;}
if(existing){
  if(!existing.isSymbolicLink() || await realpath(target)!==await realpath(source))throw new Error(`已有不同的 skill：${target}；未覆盖。请先合并或选择正确的 CODEX_HOME。`);
}else await symlink(source,target,'dir');
const result=spawnSync('codex',['mcp','add','distvis','--env',`DISTVIS_URL=${process.env.DISTVIS_URL || 'http://localhost:3000'}`,'--',process.execPath,join(root,'mcp/server.js')],{stdio:'inherit'});
if(result.error)throw result.error;
if(result.status!==0)process.exit(result.status || 1);
console.log(`Skill → ${target}\nMCP → ${join(root,'mcp/server.js')}\n重新打开 agent 会话以加载新配置。`);
