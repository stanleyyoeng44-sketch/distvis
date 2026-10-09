# DistVis

用户开发文档：[Go API](docs/GO-API.md) · [平台 HTTP API](docs/PLATFORM-API.md) · [Agent / MCP / skill](docs/AGENT.md) · [Lab 1 实验指南](docs/LAB1.md)（[English](docs/LAB1.en.md)）。启动平台后，从侧栏「API 文档」打开（可视化页面中侧栏收起为导航抽屉，点击左上角 ☰ 打开）；也可直接访问 [/docs/go](http://localhost:3000/docs/go) 和 [/docs/platform](http://localhost:3000/docs/platform)，支持搜索、目录跳转、代码复制和 Markdown 下载。

文档示例验收：`node tests/docs-container-smoke.mjs` 在空闲平台运行原样下载的 Ping 程序，记录自动归档；`node tests/browser-docs-smoke.mjs` 检查文档导航、搜索、复制和移动布局（需 Playwright）。平台 API 文档中的 `demo.mjs` 可直接执行完整的创建、输入、故障和导出流程。

面向分布式系统教学的本地实验工作台：实现协议、运行节点、注入故障，并按事件逐步回放。

已实现内置教学仿真、Go SDK、Docker / Docker Desktop K8s 运行适配、拓扑消息动画、节点 / 链路操作面板、代码草稿、事件检索、历史回放和 JSON 导出。首批示例为 **Raft 选主、令牌环互斥、LWW 副本存储**。

支持 **普通 `net/rpc` 和 gRPC unary 服务**：从 Go 参数类型 / protobuf 自动生成应用输入表单，标准客户端调用自动记录请求、返回及错误。用户可指定本地协议根目录，每个子目录一个 Go module，平台自动发现、挂载并运行。接入说明见 [Go API](docs/GO-API.md)，可运行示例在 [protocols/](protocols/)。

协议入口使用 `lab.Main(configureProtocol)`（`lab` 为 `distvis/sdk/rpc`）：公共 library 负责节点初始化、消息分发、连接复用及退出清理。协议只组装服务和状态，通过 `node.RPCPeers()` 批量取得其他节点客户端，或用 `node.RPC(node.Neighbor(1))` 取得环形后继；gRPC 使用 `lab.GRPCPeers(node, pb.NewReplicationClient)` 获取标准客户端；初始令牌、选举计时等协议启动逻辑放入 `node.OnStart`。

协议名称、简介可直接写在 `main.go` 的文件头：`// distvis:name 名称`、`// distvis:description 简介`。节点启动后不会自动发消息的协议再加 `// distvis:start node-2 Application.Sync {"minMs":0} 说明`，界面会据此提示从哪里开始。入口自动识别，多入口项目可加 `// distvis:entry ./cmd/peer`。四个示例已移除独立 JSON 文件；旧 `distvis.json` 仍兼容。

## 本地启动

需要 Node.js 22 或以上。前后端不依赖第三方运行时包，无需 `npm install`。

API 未指定 `runtime` 时默认使用 Docker/Go；单元测试和离线演示可显式指定 `runtime: "simulation"`。

```bash
cd distvis
npm start
```

浏览器访问 **http://localhost:3000**，在「我的协议」打开或创建一个 Go 协议。协议工作区提供 **协议代码 / 实验列表**；在其下创建不同参数的实验后，进入实验的 **可视化 / 运行历史**。点击「运行实验」默认启动真实 Docker Go 节点；「内置参考模型」执行 JavaScript 模型，不执行协议中的 Go 代码。

```bash
# 可选：改变端口与历史保存位置
PORT=3100 DISTVIS_DATA=/absolute/path/to/history npm start
```

服务只监听 `127.0.0.1`，面向本人使用的本地课堂实验，不提供多用户认证或公网部署支持。

## 真实 Go 节点

### Docker

1. 启动 Docker Desktop，确认 `docker version` 可连接到 daemon。
2. 选择运行环境「本机 Docker · Go 节点」。
3. 创建实验后，coordinator 自动使用 `registry-1.docker.io/library/golang:1.24-alpine` → `registry-1.docker.io/library/alpine:3.21` 构建镜像，再启动指定数量的容器。显式仓库地址避免访问 Docker Hub 配置中的失效镜像加速器。
4. 每个容器以非 root 用户运行独立 Go 程序，限制为 128 MiB / 0.5 CPU / 64 PID，禁用直接网络；所有协议消息通过 SDK 和 coordinator 传递。
5. 节点崩溃实际调用 `docker kill`；恢复启动同一容器、重新运行 Go 程序。`/state` 中的持久数据保留。
6. 结束实验自动清理本次容器和镜像，保留日志、配置和源码。

首次运行需要拉取基础镜像。无需本机安装 Go。可在事件流中看到构建或容器启动错误。

构建失败时，工作台会显示具体错误及可展开的构建输出。若错误包含 `yourcode.mirror.aliyuncs.com` 和 `403 Forbidden`，这是 Docker 配置里的镜像加速器占位地址，失败发生在下载基础镜像阶段。可在 Docker Desktop → Settings → Docker Engine 中删除 `registry-mirrors` 数组里的这个地址，保留其他配置，然后 Apply & restart。项目现已显式使用官方仓库端点；修改项目代码后需要重启 `npm start`，再新建实验重试（历史失败记录不会被改写）。

如果本机无法直接访问 Docker Hub，可以指定自己的可用仓库：

```bash
DISTVIS_GO_IMAGE=registry.example.edu/course/golang:1.24-alpine \
DISTVIS_RUNTIME_IMAGE=registry.example.edu/course/alpine:3.21 \
npm start
```

上述 `registry.example.edu` 仅为示意，必须换成已经包含相应镜像、并可由本机 Docker 拉取的真实仓库。编译镜像需包含 Go 和 shell；运行镜像需提供 Alpine 兼容的 `adduser`、`sleep` 和 `killall`。这两个变量会记录在本次构建事件与 Dockerfile 中，不能填入换行或额外构建指令。

### Docker Desktop Kubernetes

先在 Docker Desktop 中启用 Kubernetes，并确认：

```bash
kubectl --context docker-desktop get nodes
```

选择「Docker Desktop K8s · Go 节点」。适配器固定使用 `docker-desktop` context；不会使用当前默认 context，也不会操作其他集群。

coordinator 为每次实验创建专用 namespace，为每个节点创建一个 Pod，使用本地构建的镜像（`imagePullPolicy: Never`）。要求 Docker Desktop 的 K8s 能看到本地 Docker 镜像。使用不共享镜像存储的 K8s 安装方式时，应先将镜像导入集群；本版不自动推送镜像仓库。

Go 程序通过 `kubectl exec -i` 启动；崩溃终止该 Pod 内的 Go 进程，恢复重新启动进程。Pod 和 `emptyDir` 保留，模拟的是节点程序崩溃；不是销毁整台 K8s 工作节点。

## 编写协议

从「我的协议」创建或打开协议，在「协议代码」页编辑 `main.go`、`protocol.go`、`go.mod` 和 protobuf 文件。支持新建、重命名、删除文件；初始代码仅在创建协议时选择。一个协议管理一份 Go 项目，在「实验列表」中可以创建正常网络、慢网络、不同节点数量等实验。

每个实验分别保存节点数、延迟、带宽、运行环境和参考模型种子。创建实验只保存配置，不启动节点；打开实验后可「运行实验」或「仅保存配置」。运行自动进入可视化，应用输入、故障注入与消息记录归入本次运行；「运行历史」仅包含当前实验。顶部面包屑可返回所属协议，实验中的「查看协议代码」直接打开同一份共享代码。

协议代码需先保存，再运行实验；修改对所有下属实验的后续运行生效。过去的代码快照、参数与执行记录不变，支持回放、导出和只读查看。「用此版本继续编辑」把历史代码复制为所属协议的草稿，保存后供该协议下所有实验使用。不同协议之间的代码独立。

未保存草稿按协议 ID 缓存在浏览器。旧工作区保留实验 ID、参数和历史；来源及代码相同的工作区归入同一协议，不同代码保留为独立协议。旧浏览器的实验草稿分别恢复为独立协议，避免冲突草稿覆盖共享代码，原缓存仍保留。

在「新建协议 → 从本地协议目录发现代码」指定项目根目录，默认是 `protocols/`，也可设置 `DISTVIS_PROTOCOL_DIR`。每个子目录一个 Go module，支持第三方依赖及 protobuf 生成代码；选中后形成协议的可编辑版本，保存不覆盖原目录。详情见 [Go API](docs/GO-API.md)。

所有节点默认使用协议代码；高级设置仍可配置节点副本和构建入口，每次运行归档全部文件。示例入口使用 `lab.Main`，RPC 自动生成应用输入表单；参考模型运行附带 Go 参考项目，但不执行它。

以下低层 SDK 方式仍可用于直接研究消息传递：

```go
package main

import (
    "context"
    "distvis/sdk"
)

func main() {
    node, err := sdk.Open()
    if err != nil { panic(err) }
    must := func(err error) { if err != nil { panic(err) } }

    must(node.Report(map[string]any{"role": "ready"}))
    if node.ID == "node-1" {
        must(node.Send("node-2", map[string]any{
            "type": "Hello", "text": "你好",
        }))
    }
    for {
        msg, err := node.Receive(context.Background())
        if err != nil { return }
        must(node.Report(map[string]any{
            "lastFrom": msg.From, "payload": string(msg.Payload),
        }))
    }
}
```

- `node.ID` / `node.Nodes`：本节点和所有节点的逻辑地址，如 `node-1`。不需要管理 IP、端口或 DNS。
- `Send(to, payload)`：提交 JSON 消息；成功表示提交给 coordinator，不保证送达。
- `Receive(ctx)`：读取消息并自动记录接收确认。
- `Report(state)`：汇报任意 JSON 可序列化状态，立即出现在节点操作面板。
- `DeclareInput([]sdk.InputAction)`：节点程序声明应用层输入，浏览器自动生成表单；通过 `node.Commands` 接收 `{id, action, values}`，`cmd.Values` 是保留数字、布尔值、对象等类型的 `json.RawMessage`。
- `sdk.Save(value)` / `sdk.Load(&value)`：将协议状态写入工作目录的 `state.json`。示例持久化 Raft term/votedFor。
- stdout 是 SDK 的 JSON 协议通道；普通调试日志写 stderr。

### 协议声明应用输入

点击节点（或节点左上角的蓝色「运行」徽标，直接进入第一个输入框），在弹出面板的「在 node-x 运行」中填写并点「在 node-x 运行」。目标节点字段（Go 字段标签 `distvis:"node"` / `distvis:"nodes"`）显示为节点下拉列表，后者可多选。节点声明多个操作时，先选择操作再填写。输入作用于正在运行的实时节点；暂停或查看过去不会把输入发送到过去。离线节点和已结束实验不可提交。输入声明、完整字段、输入失败和后续协议消息均进入实验历史；时空图用蓝色菱形标记应用输入。

RPC 协议只需标记应用服务：字段自动从方法签名提取，既不写 `DeclareInput`，也不手工收取 `Commands`。下面的显式声明保留给低层消息 SDK 和特殊表单需求。

表单不按协议名写死：低层 Go 协议可调用 `DeclareInput`，内置模型在 `server/protocols.js` 的 actor 中声明 `inputs` 并实现 `command`。不同节点可以声明不同动作，也可以再次声明以更新接口。支持 `text`、`number`、`boolean`、`select`、`json` 字段及必填、数字范围/整数、文本长度约束。平台按声明校验字段，不隐式转换字符串为数字。最多 64 个动作、每个动作 16 个字段，单次输入最多 8 KiB。

```go
must(node.DeclareInput([]sdk.InputAction{{
    Action: "propose", Label: "提交数字",
    Fields: []sdk.InputField{{
        Name: "number", Label: "Proposed number",
        Type: "number", Required: true, Integer: true,
    }},
}}))
// 将 Commands 分支放入协议自己的 select 事件循环。
for cmd := range node.Commands {
    if cmd.Action != "propose" { continue }
    var input struct { Number float64 `json:"number"` }
    if err := json.Unmarshal(cmd.Values, &input); err != nil { continue }
    // 在此执行协议逻辑；发送网络消息用 Send，上报结果用 Report。
    must(node.Report(map[string]any{"inputId": cmd.ID, "proposed": input.Number}))
}
```

通用 HTTP 接口：`POST /api/runs/:id/commands`，请求如 `{"node":"node-2","action":"propose","values":{"number":42}}`，响应含 `commandSeq` 和 `id`。HTTP 成功表示输入已交给执行通道，应用处理/共识结果以节点状态为准。旧 LWW 的 `{node,key,value}` 请求仍兼容。

内置示例：

- **令牌环**：每个节点可提交 payload，等待持有令牌时处理；再次提交会替换未处理的 payload。状态显示待处理和最近处理的输入，下一条 Token 消息携带该 payload。提交输入不会生成新令牌。
- **Raft 选主**：任意节点提交数字，等待 Leader 后转发；分区时保持等待并重试，Leader 收到后回复。仅演示提案入口和路由，**“Leader 已接收”不表示多数派提交**，仍不包含 Raft 日志复制。
- **LWW**：声明键/值输入，继续支持向任意副本写入。
- **自定义 Go**：Hello 示例包含自定义消息输入；可自行修改声明和处理逻辑，无需修改前端或 coordinator。

已有历史保持原样；新声明和更新后的示例代码在新建实验时生效。

参考 [examples/node/main.go](examples/node/main.go) 和 [examples/custom.go.txt](examples/custom.go.txt)。

## 故障与回放

- 节点崩溃 / 恢复。
- 任意两节点之间的**有向链路**中断、固定延迟和带宽限制；可以选择 A→B、B→A 或双向。只修改一个方向时，另一方向的规则保持不变；两个方向规则不同时选择「双向」会先提示覆盖。
- 所有针对某个节点或链路的操作都在同一个弹出面板中完成：点击节点，面板依次显示应用输入、崩溃 / 恢复、与其他节点的链路和当前状态；点击连线（或面板中的链路行）直接编辑这条链路，无需再次选择目标。Esc 逐层关闭：链路 → 节点 → 面板。手机宽度下面板变为底部抽屉。
- 「恢复全部链路」和最近故障记录位于顶部控制条的「网络」菜单，随时可用。它只重置链路的延迟、带宽和中断，不会恢复已崩溃的节点。
- 面板中的操作按钮、链路规则和最近故障始终读取实时状态，并标注「实时」；节点状态行跟随回放位置，并标注「回放 @ 时间」。
- 带宽单位为 **KiB/s**，按消息 JSON 的 UTF-8 字节数计算，同方向消息排队，两个方向独立。
- 故障在运行过程中生效，已在途消息会检查接收时的链路及节点代次。崩溃前的消息不会在重启后突然恢复。
- 暂停、慢放、单步和拖动时间轴只改变**观察位置**，后台协议仍在运行。故障和写入始终作用于当前实时实验。
- 慢放支持 **0.01×、0.025×、0.05×、0.1×**，以及原来的 0.25×–4×。0.01× 表示真实经过 100 秒才回放 1 秒记录。在实时观察中选择速度会切换为回放；「回到实时」恢复追踪最新记录。
- 「拓扑动画 / 时空图」可随时切换，共用同一播放位置和速度。时空图时间从左到右，每个节点独占一行。按住图面左右拖动、横向滚轮或 Shift + 滚轮可平移，也可用底部滑块、箭头按钮或聚焦图面后的左右方向键；「回到播放位置」恢复跟随。平移只改变视野，不改变回放进度，也不会展示尚未回放的事件。较多节点可以上下滚动。
- 时空图默认每屏 1 秒，可缩放到 100/250/500 毫秒或 2/5/10/30 秒。支持按节点筛选、隐藏心跳与回复；重叠标签自动隐藏，悬停或聚焦显示，点击箭头查看完整内容。重复的相同状态上报只从图中省略，原始日志保留。
- 时空图显示截至回放位置的发送、接收、丢弃、状态与故障：实线表示已接收，虚线表示尚未接收，红色 × 表示丢弃。每个窗口最多显示最近 250 条消息、150 个状态变化/故障标记，并显示实际计数；可放大时间轴或用事件流查看其他记录。
- 拓扑消息使用方向箭头，标签包含 payload 关键字段；点击箭头暂停在当前观察位置，查看发送/接收节点、消息 ID、时间和 payload。节点操作面板按字段展示主要状态，完整 JSON 可展开查看。时空图中的节点、消息和状态标记也可点击，支持键盘 Tab / Enter。
- 事件按 coordinator 时间和单调序号排序，发送/接收通过消息 ID 关联。动画短消息采用至少 550ms 的展示时间便于观察；准确时刻以事件日志为准。
- 历史回放重建记录中的状态，不重新执行用户代码。固定种子可复现内置仿真；真实 Go 程序不保证可重复调度。

协议保存至 `data/protocols/<protocol-id>.json`，包含 Go 项目、来源与版本号。实验保存至 `data/experiments/<experiment-id>.json`，包含 `protocolId`、参数与版本号。每次运行独立保存至 `data/<run-id>/`，通过 `config.experimentId` 和 `config.protocolId` 关联，并记录执行时的 `protocolRevision`：

```text
meta.json       所属实验 ID、运行名称、协议、节点数、网络默认值、种子与运行状态
events.jsonl    消息、状态、故障、用户写入和生命周期事件
source.json     本次不可变的多文件代码快照与节点副本
build/          容器实验的实际构建上下文
```

coordinator 重启后读取历史；异常中断的运行标记为 `interrupted`，可继续回放已有轨迹，但不自动恢复执行。硬退出后容器可能仍存在，可按 `distvis.run=<run-id>` 标签检查 Docker 资源，或检查对应的 `distvis-<run-id前8位>` namespace 后清理。

## 示例范围

| 示例 | 展示内容 | 明确不包含 |
| --- | --- | --- |
| Raft 选主 | 随机超时、任期、每任期投票、多数派选举、心跳、故障重选 | 日志复制、提交、快照；不能用它演示完整 Raft 存储安全性 |
| 令牌环互斥 | 令牌交接、临界区、崩溃后的停止进展 | 令牌丢失检测与重建 |
| LWW 副本存储 | `(逻辑版本, writer)` 决胜的寄存器、周期反熵、分区后的收敛、浏览器客户端写入 | 线性一致性、事务、删除语义 |

教学仿真与 Go 示例实现同样的教学规则，但不是同一个执行引擎。SDK 的网络故障仅作用于 SDK 消息，不等价于内核级 TCP/UDP 故障。K8s 中绕过 SDK 的自建网络不受控制。

限制：2–12 节点、默认最多 10 分钟或 60,000 事件；单条节点输出最多 64 KiB、每个进程会话总 stdout 4 MiB。用于控制本地实验的资源消耗。
