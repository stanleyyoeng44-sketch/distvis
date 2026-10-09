---
name: distvis-protocol
description: 编写、修改和验证 DistVis 分布式教学协议（Go net/rpc 或 gRPC），将目录项目同步到平台，并通过真实 Docker/Kubernetes 节点检查应用输入、消息、状态和故障行为。用户要求新增协议、实现分布式算法或调试 DistVis 节点时使用。
---

# DistVis 协议开发

目标是可运行的 Go 协议、独立实验配置和有证据的运行结果。每个协议是当前协议根目录下的一个 Go module；平台的协议项目保存源码快照，实验保存参数，运行保存不可变历史。

## 获取上下文

优先调用 `distvis_catalog` 确认当前根目录、已有协议与活动运行；调用 `distvis_guide(topic="go")` 阅读当前 SDK，再按需要读取 `topic="platform"`。文档也可从本地平台 `/docs/go.md` 和 `/docs/platform.md` 获取，默认地址 `http://localhost:3000`（可通过 `DISTVIS_URL` 配置）。

MCP 不可用时，用 HTTP API 和文件工具完成相同工作；API 文档地址可直接读取，不必重新询问用户如何接入。coordinator 未启动时，在 DistVis 仓库运行 `npm start`；Docker/Kubernetes 未就绪时保留代码，说明尚未验证的部分。

## 编写协议

- 新建时用 `distvis_scaffold_protocol` 创建目录；已有目录直接编辑，不再创建同名协议。脚手架是 RPC Ping，只是可编译的起点，必须实现用户要求的算法后才算完成。
- 每个目录有 `go.mod`、`package main` 及业务代码。入口通常只需 `func main() { lab.Main(configure) }`，导入 `lab "distvis/sdk/rpc"`。节点启动、信号、连接缓存和清理交给 Runtime。
- `node.ID`、`node.Nodes` 是 coordinator 提供的成员信息；使用 `node.RPCPeers()`、`node.RPC(peer)`、`node.Neighbor(1)` 或 `lab.GRPCPeers(node, pb.New…Client)`。不要自己实现节点发现或重复启动容器，不关闭 Runtime 拥有的连接。
- setup 只组装状态、取得客户端、注册服务；主动选举、心跳、初始消息放到 `node.OnStart(func(ctx context.Context) error {...})`。setup 中不等待 RPC；多个 OnStart 按顺序执行，长循环会阻塞后续钩子。
- 内部方法是普通 net/rpc 或 gRPC unary 方法。仅应用服务加 `lab.Application()` 标记，或给 `node.GRPC` 传入 protobuf 完整应用服务名；表单来自请求类型，不手写平台协议分支、Send/Receive 包装或 schema。
- `// distvis:name ...`、`// distvis:description ...`、可选 `// distvis:entry ./cmd/peer` 放在一个 Go 文件的 `package main` 前，不新建 distvis.json。不要在多个文件重复元数据。
- `node.Report` 上报完整且有教学意义的状态对象；业务并发需要加锁。持久状态用 `sdk.Save/Load`，Report 不保存恢复状态。诊断日志写 stderr，stdout 是平台传输通道。
- 共识、互斥、恢复和重试是协议职责，库不会自动保证。明确实现范围，例如只做选主时不能称为已实现 Raft 日志提交。真实 Go 执行不是确定性模拟。

通常保留简短 main，将算法放在 protocol.go 等文件；不用为少量代码强制增加文件层级。普通 RPC 示例在文档中；仓库 `protocols/` 中的程序是可阅读的普通目录项目，无需独立的教学模板创建步骤。gRPC 项目须提交生成的 `.pb.go`，平台不会运行 protoc。

## 同步与验证

1. 在协议目录实现代码，运行 gofmt；本机编译需 `go mod edit -replace=distvis=/DistVis仓库的绝对路径` 和 `go mod tidy`。容器编译自动注入 replace。按算法需要检查并发与故障正确性，避免仅验证代码形状。
2. 用 `distvis_import_protocol(directoryId=...)` 首次导入。后续先 `distvis_get_protocol` 取得 revision，再用同一 protocolId + revision 同步。目录编辑不会自动更新平台快照，平台编辑也不会写回源目录；若两处都有修改，先比较并合并。不要为每次测试重复创建协议；createCopy 仅在用户要独立副本时使用。节点专用副本会保留，要分别检查。
3. `distvis_create_experiment` 创建所需节点数和网络参数。默认 acceptance=true 将验收实验归档；用户日常使用的实验设为 false。首次协议通常保持可见（import 默认 acceptance=false）；完全临时的验收协议可设为 true。
4. 确认没有无关活动运行，调用 `distvis_start_run`。选择 Docker 或 Kubernetes 验证真实 Go，不能用 simulation 证明代码实际执行。启动是异步的，轮询 `distvis_inspect_run`，starting 时读构建日志，failed 时报告具体错误。等待目标节点新的 input_schema，再发应用输入；有上限地等待，不无限重试构建。
5. 从最新节点 inputs 获取 action 和字段，用 `distvis_input` 提交。随后从事件中找到 `command_result.commandId == 返回的 id`，检查 error 和业务结果；HTTP/MCP 提交成功不代表算法完成。用 after=nextAfter 分页；hasMore=true 时继续读取，状态/schema 汇总总是最新的。自定义 types 过滤只改变事件页。
6. 按用户要求用 `distvis_fault` 注入故障，检查协议应满足的性质、消息内容及状态演变。只结束自己创建或用户指定的运行；starting 不能 stop。结束后给出协议/实验链接、运行 ID、实际验证的行为及未验证的边界，保留历史。

MCP 不提供任意 shell 工具：用 agent 自身的文件/终端工具写代码，MCP 负责目录骨架、平台同步、运行和观察。新的协议不需要修改平台的协议枚举、前端表单或可视化实现。
