# Agent 接入

MCP 和 skill 支持在目录中编写 Go 协议、同步平台快照、运行真实节点并检查结果。示例就是 `protocols/` 下的普通项目，平台不再单独展示教学模板入口。

## 安装

先在 DistVis 仓库安装依赖并启动 coordinator：

```bash
npm install
npm start
```

在另一个终端注册到本机 Codex：

```bash
npm run agent:install
```

此命令注册/更新名为 distvis 的 stdio MCP，并把仓库的 `skills/distvis-protocol` 链接到 `~/.codex/skills/distvis-protocol`。支持 CODEX_HOME；已有不同的同名 skill 会报错，不覆盖。重新打开 agent 会话加载配置。保留这个仓库目录，MCP 和 skill 会直接读取其中最新文件。

需要其他本地端口时，安装前设置 DISTVIS_URL，例如：

```bash
DISTVIS_URL=http://localhost:3100 npm run agent:install
```

平台没有公网鉴权，本接入面向本机 coordinator。脚手架工具直接在 coordinator 返回的协议根目录写文件，MCP 与 coordinator 应运行在同一台机器。

其他支持 MCP 的客户端可添加以下配置，替换仓库绝对路径：

```json
{
  "mcpServers": {
    "distvis": {
      "command": "node",
      "args": ["/你的路径/distvis/mcp/server.js"],
      "env": {"DISTVIS_URL": "http://localhost:3000"}
    }
  }
}
```

如果客户端找不到 node，将 command 改为 node 可执行文件的绝对路径。手工测试可用 `npm run --silent mcp`；MCP 使用 stdin/stdout 通信，直接启动后等待输入是正常的。

下载 [SKILL.md](/docs/distvis-protocol/SKILL.md)，放入客户端技能目录的 `distvis-protocol/` 文件夹即可。skill 是单文件，不需要另拷一份 API 文档。

## 给 agent 的任务示例

```text
用 DistVis 实现一个 Ricart–Agrawala 分布式互斥协议。
每个节点提供 RequestCriticalSection 输入，允许输入 payload；
展示 Lamport 时钟、请求队列、延迟回复列表和临界区状态。
放到当前协议目录下，启动三个真实 Docker 节点，验证互斥和链路中断时的等待行为。
```

Codex 可显式使用 `$distvis-protocol`，也可以按任务自动匹配。MCP 还提供 `create_protocol` prompt，参数 description 填算法需求。

agent 先读当前 Go API，再使用 RPC Ping 骨架或已有目录实现算法。骨架不是目标算法的完成实现；需要真实运行结果才能报告通过。

## MCP 工具

| 工具 | 主要参数 | 用途 |
| --- | --- | --- |
| `distvis_guide` | topic: go / platform / agent | 读取当前 API 或技能正文 |
| `distvis_catalog` | 无 | 当前根目录、目录项目、已导入协议、活动运行 |
| `distvis_scaffold_protocol` | folder、name、可选 description | 新建独立 Go module，拒绝覆盖已有目录 |
| `distvis_import_protocol` | directoryId；更新时 protocolId、revision | 目录代码同步到平台，返回代码页链接 |
| `distvis_get_protocol` | protocolId | 完整源码快照、revision 及关联实验 |
| `distvis_create_experiment` | protocolId、name、可选 runtime/nodeCount/latency/bandwidth/seed/acceptance | 创建独立实验配置，不启动 |
| `distvis_start_run` | experimentId、可选 name | 异步构建并运行当前已保存源码 |
| `distvis_inspect_run` | runId、可选 after/limit/types | 最新节点状态和输入 schema、分页事件 |
| `distvis_input` | runId、node、action、values | 提交应用输入，返回用于关联结果的 id |
| `distvis_fault` | runId、fault | 节点崩溃/恢复、链路配置或 heal |
| `distvis_stop_run` | runId | 结束自己创建或用户指定的运行，保留历史 |

运行环境为 docker 或 kubernetes，默认 docker、3 节点、80 ms 延迟和 128 KiB/s。MCP 不把内置参考模型当成 Go 代码验收。

文档资源 URI 为 `distvis://docs/go`、`distvis://docs/platform` 和 `distvis://docs/agent`。资源和工具读取相同的当前文件。

## 编写与同步

新协议目录包含 go.mod 和 main.go。使用文件编辑工具实现业务代码，MCP 不提供任意 shell 或 Go 代码生成模型。推荐 `lab.Main(configure)`，节点连接交给 Runtime；应用接口来自 RPC 类型，元数据来自 Go 头注释，详见 [Go API](/docs/go)。

首次导入示例：

```json
{"directoryId":"directory:my-lock","name":"我的互斥协议"}
```

修改目录代码后，读取协议 revision，再同步到同一个平台项目：

```json
{"directoryId":"directory:my-lock","protocolId":"已有协议 UUID","revision":2}
```

首次导入默认 acceptance=false，协议正常显示；验收实验默认 acceptance=true，会归档在该协议下。给用户长期使用的实验设 acceptance=false。协议已归档时，其新实验也会归档。

同一目录已导入过时，工具会返回已有 ID，避免误建重复协议。明确需要独立副本时才能使用 createCopy=true。更新保留既有节点专用代码副本，只替换 shared 项目；revision 冲突应读取后比较合并。

平台保存独立源码快照。目录编辑不会自动改变平台代码，平台编辑也不会写回目录。运行使用已同步版本，历史不受后续修改影响。

## 验证与观察

start_run 返回不代表构建成功。用 inspect_run 检查 starting/running/failed，并等待目标节点 input_schema；从节点 inputs 取 action 和字段，不能硬编码自动生成的动作 ID。

输入提交成功后，在事件中等待 `command_result.commandId` 等于返回 id，再检查 error 和 result；还要依据协议状态验证互斥、共识或收敛等要求。

inspect_run 默认每页 100 条，最多 500 条。继续读取时设置 after=上一次 nextAfter，hasMore=true 表示当前还有历史页。types 可以只取 command_result、fault、state 等类型；nodes 汇总不受事件过滤影响，始终表示实时最新状态，不是回放到 after 时的状态。exportUrl 可下载完整历史。

```json
{"runId":"运行 UUID","after":0,"limit":100,"types":["runtime","command_result","fault"]}
```

故障参数与 [平台 HTTP API](/docs/platform) 相同，例如：

```json
{"runId":"运行 UUID","fault":{"kind":"link","from":"node-1","to":"node-2","latency":500,"bandwidth":128,"blocked":true,"bidirectional":true}}
```

平台一次只允许一个活动运行。MCP 不会为启动新协议自动停止其他运行；starting 阶段不能 stop。结束验收后保留运行 ID、链接和观察结果，便于在页面中回放。
