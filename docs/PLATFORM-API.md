# 平台 HTTP API

用于脚本、教学工具和自定义前端管理协议、配置实验、执行运行、注入故障并读取日志。默认地址 `http://localhost:3000`。请求和响应使用 JSON；实时事件使用 SSE。

## 对象与基本约定

| 对象 | ID | 所有权 |
| --- | --- | --- |
| 协议项目 | `protocolId` | 一份可编辑 Go 项目，可包含多个实验 |
| 实验 | `experimentId` | 所属协议、参数和独立运行历史 |
| 一次运行 | `runId`，响应字段为 `id` | 执行时的参数、源码快照、消息、状态、输入与故障 |

协议模板键如 `raft`、`token`、`gossip`、`grpc`、`directory:folder` 与协议项目 UUID 不同。`/api/protocols` 是来源目录；`/api/protocol-projects` 才是用户创建的协议项目。

POST 请求设置 `Content-Type: application/json`。没有 token 或多用户鉴权，服务默认监听 127.0.0.1。带 Origin 的写请求必须与 `http://请求Host` 完全一致。列表接口没有分页；“归档”只改变展示分类，不删除对象，也不禁止读取或运行。

所有名称最多 100 个字符。协议和实验更新必须携带最新 revision。修改协议代码影响其下实验的后续运行，已保存的运行快照不变。平台同时只允许一次运行处于 starting/running。

## 完整调用示例

保存为 `demo.mjs`，执行 `node demo.mjs`（Node.js 22+）。需要 coordinator 已启动、Docker 可用、当前没有活动运行。脚本创建归档验收记录，运行真实 Go 令牌环，提交 payload，模拟节点故障，最后结束本次运行。

```js
const base = 'http://localhost:3000';
async function api(path, body) {
  const res = await fetch(base + path, body === undefined ? {} : {
    method: 'POST',
    headers: {'Content-Type': 'application/json', 'X-Distvis-Purpose': 'acceptance'},
    body: JSON.stringify(body),
  });
  const data = await res.json();
  if (!res.ok) throw new Error(`${res.status}: ${data.error}`);
  return data;
}
async function waitFor(check, timeout = 600000) {
  const end = Date.now() + timeout;
  while (Date.now() < end) {
    const value = await check();
    if (value) return value;
    await new Promise(resolve => setTimeout(resolve, 250));
  }
  throw new Error('等待超时；请在平台检查运行状态');
}
const protocol = await api('/api/protocol-projects', {
  name: 'API 示例 · 令牌环', protocol: 'token',
});
const experiment = await api('/api/experiments', {
  name: '三节点实验', protocolId: protocol.id,
  settings: {runtime: 'docker', nodeCount: 3, latency: 80, bandwidth: 128, seed: 42},
});
const run = await api('/api/runs', {experimentId: experiment.id, name: '第一次运行'});
const path = `/api/runs/${run.id}`;
console.log('运行 ID:', run.id);
try {
  const ready = await waitFor(async () => {
    const r = await api(path);
    if (['failed', 'completed', 'interrupted'].includes(r.status)) {
      throw new Error(`运行已结束: ${r.status}`);
    }
    return r.status === 'running' && r.events.some(e => e.type === 'input_schema' && e.node === 'node-1') && r;
  });
  const schema = ready.events.findLast(e => e.type === 'input_schema' && e.node === 'node-1').schema;
  const submit = schema.find(s => s.label === 'Application.Submit');
  if (!submit) throw new Error('当前协议没有 Application.Submit');
  const command = await api(path + '/commands', {
    node: 'node-1', action: submit.action, values: {Payload: 'hello'},
  });
  const result = await waitFor(async () => (await api(path)).events.find(
    e => e.type === 'command_result' && e.commandId === command.id
  ), 30000);
  console.log('应用返回:', result);
  await api(path + '/faults', {kind: 'crash', node: 'node-2'});
  await api(path + '/faults', {kind: 'recover', node: 'node-2'});
} finally {
  const current = await api(path);
  if (current.status === 'running') await api(path + '/stop', {});
  // starting 状态不能 stop。若启动等待超时，稍后从平台检查并结束。
}
const archive = await api(path + '/export');
console.log('归档事件数:', archive.events.length);
```

`X-Distvis-Purpose: acceptance` 让新建的协议和实验自动归档，避免验收记录混入正常列表。日常脚本可以省略此头。对已存在实验发起运行时，此头不会改变已有实验的归属或归档状态。

## 发现模板与本地目录

| 方法与路径 | 请求 | 成功响应 |
| --- | --- | --- |
| `GET /api/examples` | 无 | 内置 Go 项目数组：id、name、description、directory、entry 等 |
| `GET /api/protocols` | 无 | 以来源键索引的内置模型/本地项目对象；grpc 模板应从 examples 获取 |
| `GET /api/library` | 无 | `{root, entries, errors}`，每次重新发现目录 |
| `POST /api/library` | `{root:"/绝对路径/my-protocols"}` | 同 GET，设置持久化的根目录 |
| `GET /api/project?protocol=token` | URL 编码的来源键 | 一个完整项目快照，包括 base64 文件、entry 和 sha256 |
| `GET /api/example` | 无 | `{source}`，兼容旧单文件 SDK 示例；新客户端用 examples/project |

根目录的每个直接子目录应是一个 Go module；错误项目列入 errors，不阻止其他项目发现。Go 文件头元数据和入口规则见 [Go API](/docs/go)。从响应中取得 `directory:...` 键，不自行拼接非 ASCII 文件夹名称。

## 协议项目

| 方法与路径 | 行为 |
| --- | --- |
| `GET /api/protocol-projects` | 项目摘要数组，不含 projects；包括 experimentCount、archivedExperimentCount、runCount、mergedIds |
| `POST /api/protocol-projects` | 创建，201 返回完整对象 |
| `GET /api/protocol-projects/{protocolId}` | 读取完整对象及 projects |
| `POST /api/protocol-projects/{protocolId}` | 更新名称、代码或归档状态，200 返回更新后完整对象 |
| `GET /api/protocol-projects/{protocolId}/experiments` | 该协议的实验摘要数组，含归档实验 |

从模板创建：

```json
{"name":"我的令牌环","protocol":"token"}
```

从用户代码创建：

```json
{
  "name":"我的 Go 程序",
  "protocol":"custom",
  "projects":{
    "shared":{
      "entry":".",
      "files":[
        {"path":"go.mod","content":"module lesson/custom\n\ngo 1.24\n\nrequire distvis v0.0.0\n","encoding":"utf8"},
        {"path":"main.go","content":"package main\nfunc main() {}\n","encoding":"utf8"}
      ]
    }
  }
}
```

上述空 main 只展示文件传输格式；可运行的节点示例见 Go API。协议完整对象主要字段为 id、name、protocol、projects、revision、createdAt、updatedAt、archived 和可选 origin。`archived` 在部分旧记录中省略，按 false 处理。

更新请求只提交想改的字段及 revision：

```json
{"revision":2,"name":"令牌环实验代码","archived":false}
```

提交 projects 会整体替换该协议所有代码项目，不是文件级 patch；先 GET，再修改并提交。`projects.shared` 必填；`projects["node-2"]` 等可选节点副本完整替换该节点代码，没有副本的节点使用 shared。最多支持 node-1 到 node-12。

协议合并产生的旧 ID 会解析到当前协议，响应 id 可能不同。客户端应采用返回的 id。不存在的协议、陈旧 revision 和非法项目目前均返回 400，而非 404/409。

## 文件快照与预校验

| 字段 | 说明 |
| --- | --- |
| `entry` | 构建包目录，例如 `.`；不提供时根据注释/结构推断 |
| `files[].path` | 项目内相对路径，支持子目录，不支持绝对路径或 `..` |
| `files[].content` | utf8 文本或 base64 字符串 |
| `files[].encoding` | `utf8` 或 `base64`；未声明时默认 utf8 |
| 项目级 `encoding` | GET 返回的归档通常为 base64；保存 projects 时支持项目级默认编码 |
| `sha256` | 服务端计算的文件集合摘要；不包含构建入口，不作为代码和入口同时相等的唯一依据 |

快照中的文件另含 size、sha256；服务端会重新计算，不信任客户端提交的摘要。二进制文件用 base64。

```text
POST /api/workspace/validate
```

```json
{"workspace":{"entry":".","files":[{"path":"go.mod","content":"module demo\n"},{"path":"main.go","content":"package main\nfunc main(){}\n"}]},"nodeWorkspaces":{}}
```

返回 `{"ok":true,"sha256":"...","fileCount":2}`。仅校验项目结构与元数据，不编译、不检查第三方依赖是否可下载。nodeWorkspaces 可选，键为节点 ID。此低层预校验接口使用文件级 encoding；把 GET 的 base64 快照传入时，应给每个文件显式设 `encoding:"base64"`。

单个项目最多 5000 文件、总内容 32 MiB，请求体最多 48 MiB。必须有 go.mod 和入口 main 包。项目归档不接受符号链接；路径段只支持字母数字、下划线、点和连字符，不接受 `.git`、`.codegraph`、node_modules 等保留路径。

## 实验与参数

| 方法与路径 | 行为 |
| --- | --- |
| `GET /api/experiments` | 所有实验摘要数组，包括 runCount、latestRun |
| `POST /api/experiments` | 创建实验，201 返回实验对象，不启动节点 |
| `GET /api/experiments/{experimentId}` | 读取实验对象，不包含代码 |
| `POST /api/experiments/{experimentId}` | 更新 name、settings、archived，必须提交 revision |
| `GET /api/experiments/{experimentId}/runs` | 该实验的运行摘要，按 createdAt 倒序 |

推荐创建方式：

```json
{"name":"慢速网络","protocolId":"协议 UUID","settings":{"runtime":"docker","nodeCount":5,"latency":500,"bandwidth":128,"seed":42}}
```

实验包含 id、name、protocolId、protocol 来源、settings、revision、createdAt、updatedAt、archived。代码只能在所属协议中保存；给实验提交 projects 会报错。实验不能通过更新请求改属另一个协议。

| settings 字段 | 默认值 | 范围与单位 |
| --- | --- | --- |
| `runtime` | `docker` | docker、kubernetes、simulation |
| `nodeCount` | 5 | 2–12 的整数 |
| `latency` | 80 | 0–30000，毫秒 |
| `bandwidth` | 128 | 1–100000，KiB/s，每条有向链路 |
| `delayModel` | `fixed` | `fixed` 固定延迟；`exponential` 在 latency 之上为每条消息再加指数分布抖动 |
| `jitter` | 0 | 0–30000，毫秒；exponential 时为抖动均值，须 ≥1；fixed 时保存为 0 |
| `seed` | 42 | 1–2147483647 的整数，控制内置模型和延迟抖动的随机序列 |

指数抖动：消息的传输时间为「带宽排队 + latency + Exp(jitter)」，抽样取整到毫秒并记录在 send 事件的 `jitter` 字段。每条有向链路仍保持 FIFO（与 TCP 相同）：抖动只会推迟后续消息，不会让它们越过先发的消息。平台每 50 ms 推进一次消息投递，实际到达时间另有最多约 50 ms 的量化误差。

提交 settings 是整体替换，省略字段会回到默认值；要修改一个参数，请先 GET，再合并原 settings 后提交。`simulation` 仅用于 raft、token、gossip 来源对应的内置模型；它不执行协议 Go 代码。

```json
{"revision":1,"settings":{"runtime":"docker","nodeCount":5,"latency":200,"bandwidth":128,"seed":42}}
```

归档/恢复只需 `{revision, archived:true/false}`。协议归档不会逐个改写已有子实验的 archived 字段；在已归档协议下新建实验默认归档。读取列表后应按需求过滤。

兼容旧客户端：创建实验时也可以不提供 protocolId，而提交 protocol 来源或 projects；会同时创建一个协议。新客户端应显式分两步创建，避免产生不必要的协议副本。

## 创建与控制运行

| 方法与路径 | 行为 |
| --- | --- |
| `GET /api/runs` | 全部运行摘要；需要单个实验的历史时优先用实验 runs 接口 |
| `POST /api/runs` | 异步构建并启动，201 返回运行摘要 |
| `GET /api/runs/{runId}` | 运行摘要加完整 events 数组 |
| `POST /api/runs/{runId}/stop` | 空 JSON 请求；结束运行，200 返回摘要 |
| `GET /api/runs/{runId}/source` | 当时的 source、nodeSources 及可选 project、nodeProjects 快照 |
| `GET /api/runs/{runId}/export` | 可下载 JSON：schemaVersion=1、摘要、源代码和 events |

推荐运行请求：

```json
{"experimentId":"实验 UUID","name":"高延迟 · 第一次运行"}
```

使用所属协议当前已保存代码，服务端设置 protocolId、protocolRevision，并从实验 settings 取参数。请求可覆盖 runtime、nodeCount、latency、bandwidth、seed；覆盖只影响本次运行，不保存回实验。平台 UI 会先保存实验参数再启动，这是 UI 行为，不是 runs 接口自动保存。

摘要结构：

```json
{
  "id":"运行 UUID",
  "createdAt":"2026-09-21T08:00:00.000Z",
  "status":"starting",
  "time":0,
  "eventCount":0,
  "config":{
    "name":"第一次运行","protocol":"token","runtime":"docker",
    "nodeCount":3,"latency":80,"bandwidth":128,"seed":42,
    "experimentId":"实验 UUID","protocolId":"协议 UUID","protocolRevision":2,
    "project":{"entry":".","sha256":"..."}
  }
}
```

201 不表示 Go 已完成编译或所有服务已经就绪。轮询运行状态，或订阅事件；向某节点输入前等待它的 input_schema。构建失败时 status=failed，错误在 `runtime` 事件的 `level:error, phase:startup, message` 中。

| status | 含义 |
| --- | --- |
| `created` | 引擎内部初始状态，通常不由创建接口返回 |
| `starting` | 构建/创建节点中；目前不能 stop |
| `running` | 运行中，可以输入、注入故障和 stop |
| `completed` | 正常停止或达到时长/事件限制 |
| `failed` | 构建/启动失败 |
| `interrupted` | coordinator 重启时发现上一次尚未结束的记录 |

对 completed/failed/interrupted 再 stop 不改写终态。结束运行会清理该次容器资源，清理可能异步完成。旧运行仍可读取和回放，不支持通过 API 恢复为继续执行。

直接运行兼容接口：不传 experimentId 时，可传 protocol 来源、runtime 等参数；自定义代码可用 `protocol:"custom", workspace:{...}, nodeWorkspaces:{...}`，或旧 `source` / `nodeSources`。服务端会归入兼容实验；推荐使用协议→实验→运行流程。不要传 `protocol:"grpc"` 直接运行；从 grpc 模板创建协议/实验后运行，或使用目录来源。

## 应用输入与返回

```text
POST /api/runs/{runId}/commands
```

```json
{"node":"node-1","action":"从 input_schema 取得的 action","values":{"Payload":"hello"}}
```

成功返回 200：`{"ok":true,"commandSeq":42,"id":"input-42"}`。这表示输入已交给正在运行的节点，并非业务方法已经成功。等待 `command_result` 中 `commandId === id` 的事件；结果包含 result；error 非空表示调用失败，成功时 error 可能省略。节点崩溃或运行停止可能导致没有结果事件，客户端应设置等待期限。

从最新的 input_schema 事件按 node 取 schema；节点重启后可能重新声明。每个动作包含 action、label、可选 description 和 fields。字段包含 name、label、type，以及可选 required、integer、min、max、maxLength、options。

| type | values 中的类型 |
| --- | --- |
| `text` | string，最多 maxLength 或默认 8192 字符 |
| `number` | 有限 JSON number；integer 要求安全整数 |
| `boolean` | JSON boolean |
| `select` | options 中的 string |
| `json` | JSON 值，业务结构由协议解析 |
| `node` | 本次运行中的一个节点 ID；可选字段可为空字符串 |
| `nodes` | 本次运行中节点 ID 的数组，不可重复；必填时至少一个 |

schema 最多 64 动作、每个 16 字段、声明总计 48 KiB；values 必须为 JSON 对象、最多 8 KiB，拒绝未声明字段。标识符为字母开头的 1–64 位字母数字、下划线或连字符，字段不能为 constructor/prototype 等保留名。RPC 自动生成的动作 ID 不应硬编码。

运行必须处于 running，目标节点在线且已声明 action。故障操作期间输入可能被拒绝。应用输入始终发送到实时节点，不受浏览器回放位置影响。

### 并发输入

```json
{"batch":[
  {"node":"node-1","action":"从 node-1 的 input_schema 取得","values":{"holdMs":3000}},
  {"node":"node-3","action":"从 node-3 的 input_schema 取得","values":{"holdMs":3000}}
]}
```

用同一个 `POST /api/runs/{runId}/commands` 提交。平台先校验全部输入（节点在线、已声明 action、字段合法、节点不重复，最多 nodeCount 项），任一不合法则整批拒绝、不发送；通过后在同一协调器时刻依次交给各节点，因此这些操作彼此没有因果关系，可用来构造并发事件。返回 `{"ok":true,"batch":"batch-57","commands":[{"node":"node-1","commandSeq":57,"id":"input-57"},…]}`，某个节点在交付时失败会在对应项给出 `error`。每个 `command` 事件带 `batch` 和 `concurrentWith`（同批其他节点）；各自的结果仍按 `commandId` 等待 command_result。浏览器中，在节点面板的「在 node-x 运行」里勾选「同时在其他节点运行」即可。

兼容请求 `{node,key,value}` 使用默认 action=write；只对声明了 write 的协议有意义。

## 故障注入

```text
POST /api/runs/{runId}/faults
```

| kind | 请求字段 | 效果 |
| --- | --- | --- |
| `crash` | node | 杀死真实节点进程/容器或停止模型节点 |
| `recover` | node | 恢复该节点，同次运行的持久状态保留 |
| `link` | from、to、latency、bandwidth、blocked、bidirectional，可选 delayModel、jitter | 设置有向链路；bidirectional=true 时同步反向链路；省略 delayModel 时沿用运行的延迟分布 |
| `heal` | 无其他字段 | 清除全部链路覆盖，恢复运行默认网络；不恢复已崩溃节点 |

```json
{"kind":"crash","node":"node-2"}
```

```json
{"kind":"link","from":"node-1","to":"node-2","latency":500,"bandwidth":64,"blocked":true,"bidirectional":true}
```

```json
{"kind":"heal"}
```

link 的六个参数都必填；节点必须不同，latency/bandwidth 与实验参数使用相同整数范围，blocked/bidirectional 必须为 JSON boolean。修改后返回 `{"ok":true}`，操作进入 fault 事件；节点在线变化另有 node 事件。

只支持 running。并发故障操作返回 409。崩溃/链路中断可能使在途消息失效；恢复链路不会重新投递已经丢弃的消息。故障后的重试、协议恢复由 Go 程序处理。

## 实时事件与回放

```text
GET /api/runs/{runId}/events?after=42
Accept: text/event-stream
```

SSE 先发送全部 seq 大于 after 的已记录事件，再发送新事件。请求头 Last-Event-ID 优先于 after。每 15 秒有注释心跳；运行结束后连接不会自动关闭，客户端应按需关闭。

```text
id: 43
data: {"seq":43,"time":1500,"timestamp":"2026-09-21T08:00:01.500Z","type":"state","node":"node-2","state":{"role":"ready"}}

```

浏览器示例：

```js
const stream = new EventSource(`/api/runs/${runId}/events?after=0`);
const seen = new Set();
stream.onmessage = event => {
  const item = JSON.parse(event.data);
  if (seen.has(item.seq)) return;
  seen.add(item.seq);
  console.log(item);
  if (item.type === 'lifecycle' && item.action === 'stop') stream.close();
};
// 离开页面或不再需要时 stream.close()。
```

事件公共字段为 seq（单次运行内从 1 递增）、time（运行时钟毫秒）、timestamp（createdAt+time 的 ISO 时间）和 type。time 可相同，按 seq 确定顺序；timestamp 不是每个节点的本地时钟。内置模型用虚拟时间，真实 Go 运行用 coordinator 的经过时间。

| type | 主要字段 |
| --- | --- |
| `lifecycle` | action=start/stop，config/nodes 或 reason |
| `send` | id、from、to、payload、bytes、delay，指数抖动时另有 jitter |
| `receive` / `drop` | id、from、to，drop 的 reason；按 id 关联 send |
| `deliver` | 真实运行中交付到节点 SDK 的消息；与协议消费后记录的 receive 区分 |
| `state` | node、state，节点上报的完整状态 |
| `node` | node、online、可选 reason |
| `fault` | fault 请求对象 |
| `input_schema` | node、schema |
| `command` | node、action、values；并发输入另有 batch、concurrentWith |
| `command_result` | node、commandId、result、error |
| `command_error` | node、commandSeq、message，输入交付失败 |
| `runtime` / `stdout` / `stderr` 等 | 运行诊断；按实际事件读取 node、message、level、phase 等可选字段 |

RPC 消息 payload 中包含 type=RPCRequest/RPCResponse/RPCCancel、rpcId、protocol、method、body、wire 等。一个 RPC 的请求和响应各自有网络消息 id，用 rpcId 关联成调用；body 是可展示 JSON，wire 是 gob/protobuf 的 base64 表示。

回放是客户端消费历史事件的行为，没有“暂停协议执行”或“回到过去注入故障”的 API。完整历史可从 GET run 或 export 获取。高频观察推荐 SSE，避免反复下载整个 events 数组。

## 错误与限制

错误响应统一为 `{"error":"具体说明"}`。

| HTTP 状态 | 当前实现中的情况 |
| --- | --- |
| 400 | 参数/JSON 不合法、协议或实验不存在、revision 冲突、未运行、动作未声明、starting 时 stop、源码结构无效 |
| 403 | 浏览器跨源写入被拒绝 |
| 404 | 未知 API 路径或 run 不存在 |
| 409 | 已有运行正在构建/运行，或另一个故障操作进行中 |
| 503 | coordinator 正在关闭 |

revision 冲突要重新 GET 后合并修改，不要无条件重试覆盖。构建失败通常发生在 201 返回之后，因此不能仅靠 HTTP 创建响应判断运行成功。

每次运行最多 2–12 节点，默认 10 分钟或约 60,000 事件；这些运行上限目前不是可设置的 API 参数。项目限制见“文件快照”。协议、实验和运行暂不提供 DELETE、分页、多用户或公网认证接口。

## 数据保留与版本

协议写到 `data/protocols/<id>.json`，实验写到 `data/experiments/<id>.json`；每次运行在 `data/<run-id>/` 下保存 meta.json、events.jsonl、source.json 和构建快照。归档不删除这些文件。

API 路径目前未带版本号；export 使用 schemaVersion=1。旧运行可能缺少 protocolRevision、project 等新字段，客户端应兼容。代码的 sha256 不包含 entry，回放时应同时读取当次 entry 和源码快照。修改协议代码不会改变这些历史字段。

如何编写节点程序、注册 RPC 与上报状态，见 [Go API](/docs/go)。

## SDK RPC 网络

SDK RPC 协议的 RPC 请求和回复均经过 coordinator。实验配置支持 `network`、`networkProfiles`、`parameters` 和 `limits`：

- `lossRate`：0–1 的随机丢包率；
- `ordering`：`fifo` 或 `unordered`；
- `reorderRate`、`reorderMin`、`reorderMax`：回复或请求的额外乱序延迟；
- `failureMode`：`timeout` 或 `notify`；
- `parameters`：传给节点的 JSON 参数，最多 8KiB；
- `limits.eventCount`、`limits.durationMs`、`limits.messageBytes`：运行资源上限。

协议程序通过 `sdk.Open` 收到 `parameters`，RPC 层通过标准 `net/rpc.Client` API 调用；平台记录 `send`、`deliver`、`receive`、`drop`、状态和故障事件。测试驱动不需要拥有自己的消息网络。
