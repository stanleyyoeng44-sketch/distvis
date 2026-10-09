import {McpServer} from '@modelcontextprotocol/sdk/server/mcp.js';
import {StdioServerTransport} from '@modelcontextprotocol/sdk/server/stdio.js';
import {z} from 'zod';
import {pathToFileURL} from 'node:url';
import {createService,readGuide,documents} from './service.js';

const id=z.string().uuid(),name=z.string().trim().min(1).max(100);
const line=(max)=>z.string().trim().min(1).max(max).refine(s=>!/[\r\n\u2028\u2029]/.test(s),'必须是单行文字');
const runtime=z.enum(['docker','kubernetes']);
const latency=z.number().int().min(0).max(30000),bandwidth=z.number().int().min(1).max(100000);
const delayModel=z.enum(['fixed','exponential']),jitter=z.number().int().min(0).max(30000);
const node=z.string().regex(/^node-([1-9]|1[0-2])$/);
export function createMcpServer(base){
  const server=new McpServer({name:'distvis',version:'0.1.0'}),service=createService(base);
  function tool(key,description,inputSchema,fn,readOnly=false,destructive=false){
    server.registerTool(key,{description,inputSchema,annotations:{readOnlyHint:readOnly,destructiveHint:destructive,openWorldHint:false}},async args=>{
      try{const value=await fn(args);return {content:[{type:'text',text:typeof value==='string'?value:JSON.stringify(value,null,2)}],...(typeof value==='object'?{structuredContent:value}:{})};}
      catch(error){return {isError:true,content:[{type:'text',text:error.message}]};}
    });
  }
  tool('distvis_guide','读取当前 Go API、平台 HTTP API 或 agent 开发流程。写协议前先读 go 和 agent。',{topic:z.enum(['go','platform','agent'])},({topic})=>readGuide(topic),true);
  tool('distvis_catalog','查看当前协议根目录、发现的 Go 项目、已导入协议及活动运行。写入/运行前用它确认目标，避免重复创建和打断别人的运行。',{},()=>service.catalog(),true);
  tool('distvis_scaffold_protocol','在当前根目录创建一个独立 Go module。生成可运行的 RPC Ping 骨架；随后用文件编辑工具实现目标算法。拒绝覆盖已有目录，不自动导入或启动。',{folder:z.string().regex(/^[a-z][a-z0-9-]{0,63}$/),name:line(100),description:line(1000).optional()},args=>service.scaffold(args));
  tool('distvis_import_protocol','同步目录中的代码到平台。首次导入创建协议；更新时给 protocolId + revision。不会改写目录文件或历史快照；保留节点专用副本。默认拒绝同一目录重复导入。',{directoryId:z.string().regex(/^directory:[^/\\]+$/),protocolId:id.optional(),revision:z.number().int().positive().optional(),name:name.optional(),acceptance:z.boolean().default(false),createCopy:z.boolean().default(false)},args=>service.importProtocol(args),false,true);
  tool('distvis_get_protocol','读取已导入协议的完整文件快照、revision 及其所属实验。文件内容按项目 encoding 解码。',{protocolId:id},args=>service.getProtocol(args),true);
  tool('distvis_create_experiment','为协议创建独立参数配置，不启动运行。默认归档验收实验；用户需要日常使用的实验时设 acceptance=false。仅提供真实 Go 运行环境。',{protocolId:id,name,runtime:runtime.default('docker'),nodeCount:z.number().int().min(2).max(12).default(3),latency:latency.default(80),bandwidth:bandwidth.default(128),delayModel:delayModel.default('fixed').describe('exponential：每条消息在固定延迟上再加 Exp(jitter) 毫秒；链路仍 FIFO'),jitter:jitter.default(0).describe('指数抖动的均值，exponential 时须 ≥1'),seed:z.number().int().min(1).max(2147483647).default(42),acceptance:z.boolean().default(true)},args=>service.createExperiment(args));
  tool('distvis_start_run','用该实验所属协议的已保存快照异步构建并运行。启动前同步目录变更，平台繁忙时返回错误；不会停止其他运行。',{experimentId:id,name:name.optional()},args=>service.startRun(args));
  tool('distvis_inspect_run','读取运行状态、每个节点最新状态/应用 schema，按 seq 分页读取事件。用 nextAfter 继续读取；可过滤 types。command_result.commandId 对应输入返回 id。',{runId:id,after:z.number().int().nonnegative().default(0),limit:z.number().int().min(1).max(500).default(100),types:z.array(z.string()).min(1).optional()},args=>service.inspectRun(args),true);
  tool('distvis_input','调用一个在线节点的应用入口。action 和 values 字段从最新 input_schema 读取。成功只表示已提交，须检查 command_result。',{runId:id,node,action:z.string().min(1),values:z.record(z.unknown())},args=>service.input(args));
  tool('distvis_input_concurrent','在同一协调器时刻向多个在线节点提交应用输入，模拟并发事件；全部校验通过后才一起发出。每项返回自己的 id，分别检查 command_result。',{runId:id,inputs:z.array(z.object({node,action:z.string().min(1),values:z.record(z.unknown())})).min(1).max(12)},args=>service.inputConcurrent(args));
  tool('distvis_fault','向本次实时运行注入故障；heal 仅恢复链路，recover 恢复节点。故障会保存在历史中。',{runId:id,fault:z.discriminatedUnion('kind',[
    z.object({kind:z.literal('crash'),node}),z.object({kind:z.literal('recover'),node}),z.object({kind:z.literal('heal')}),
    z.object({kind:z.literal('link'),from:node,to:node,latency,bandwidth,blocked:z.boolean(),bidirectional:z.boolean(),delayModel:delayModel.optional(),jitter:jitter.optional()}),
  ])},args=>service.fault(args),false,true);
  tool('distvis_stop_run','结束指定的运行并保留历史。只结束自己创建或用户指定的运行；starting 时不能停止。',{runId:id},args=>service.stopRun(args),false,true);
  for(const topic of Object.keys(documents))server.registerResource(`distvis-${topic}`,`distvis://docs/${topic}`,{mimeType:'text/markdown',description:`DistVis ${topic} reference`},async uri=>({contents:[{uri:uri.href,mimeType:'text/markdown',text:await readGuide(topic)}]}));
  server.registerPrompt('create_protocol',{description:'按当前 Go API 创建并验证一个目录协议',argsSchema:{description:z.string()}},async({description})=>({messages:[{role:'user',content:{type:'text',text:`请实现这个 DistVis 协议：${description}\n\n先读取 distvis_guide(agent/go) 和 distvis_catalog，再按目录 → 实现 → 同步 → 实验 → 真实运行的流程完成。节点初始化和连接交给 lab.Runtime；RPC 请求类型声明输入，Go 头注释声明元数据。只报告实际验证的行为。`}}]}));
  return server;
}
if(process.argv[1] && import.meta.url===pathToFileURL(process.argv[1]).href){
  createMcpServer().connect(new StdioServerTransport()).catch(error=>{console.error(error);process.exitCode=1;});
}
