# Go API

用普通 net/rpc 或 gRPC unary 编写节点程序。平台提供节点名单、受控通信、日志与回放；协议负责业务状态、算法和容错。推荐包：`distvis/sdk/rpc`，本文别名为 `lab`。

## 快速开始

每个协议是一个独立 Go module，放在用户协议根目录的一个子目录中。需要 Go 1.24+。运行时选择 Docker 或 Kubernetes；“内置参考模型”不会执行你的 Go 程序。

```text
my-protocols/
  ping/
    go.mod
    main.go
```

`go.mod`：

```go.mod
module lesson/ping

go 1.24

require distvis v0.0.0
```

容器编译时平台自动注入 `replace distvis => /opt/distvis`。在本机 IDE 中编译，则执行 `go mod edit -replace=distvis=/你的绝对路径/distvis`，再 `go mod tidy`。gRPC 项目还需要正常声明 grpc/protobuf 依赖，并提交生成的 `.pb.go`。

下面是完整的 `main.go`。应用方法 `Application.Ping` 自动生成输入表单；内部方法 `Echo.Echo` 记录通信但不显示为应用入口。

```go
// distvis:name RPC Ping
// distvis:description 从任意节点向另一个节点发起 RPC，观察请求、返回和节点状态。
package main

import (
	"fmt"
	"net/rpc"
	"sync"

	lab "distvis/sdk/rpc"
)

type PingArgs struct {
	Peer string `json:"peer" distvis:"node"`
	Text string `json:"text"`
}
type EchoArgs struct {
	Text string `json:"text"`
}
type EchoReply struct {
	Node string `json:"node"`
	Text string `json:"text"`
}
type EchoService struct {
	mu       sync.Mutex
	node     *lab.Runtime
	received int
}
type Application struct {
	node  *lab.Runtime
	peers map[string]*rpc.Client
}

func (s *EchoService) Echo(args EchoArgs, reply *EchoReply) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.received++
	*reply = EchoReply{Node: s.node.ID, Text: args.Text}
	return s.node.Report(map[string]any{
		"role": "ready", "received": s.received, "lastText": args.Text,
	})
}

func (a *Application) Ping(args PingArgs, reply *EchoReply) error {
	peer := args.Peer
	if peer == "" {
		peer = a.node.Neighbor(1)
	}
	client := a.peers[peer]
	if client == nil {
		return fmt.Errorf("choose another node: %q", peer)
	}
	return client.Call("Echo.Echo", EchoArgs{Text: args.Text}, reply)
}

func configure(node *lab.Runtime) error {
	peers, err := node.RPCPeers()
	if err != nil {
		return err
	}
	if err := node.RegisterName("Echo", &EchoService{node: node}); err != nil {
		return err
	}
	if err := node.RegisterName("Application", &Application{node: node, peers: peers}, lab.Application()); err != nil {
		return err
	}
	return node.Report(map[string]any{"role": "ready", "received": 0})
}

func main() { lab.Main(configure) }
```

下载 [main.go](/docs/examples/ping/main.go) 和 [go.mod](/docs/examples/ping/go.mod)。在平台点“新建协议”，展开本地目录设置，选择 `my-protocols` 并刷新；选中 `ping` 创建协议，再新建一个 3 节点实验并运行。节点就绪后，点击 node-1，在「在 node-1 运行」中从 peer 下拉列表选 node-2，text 填 `hello`，再点「在 node-1 运行」。可以看到请求箭头、RPC 返回，以及 node-2 的 `received` 状态。peer 选「不指定」时选择环形后继。peer 字段带 `distvis:"node"` 标签，所以显示为节点下拉列表。

## 项目说明与入口

在一个 `package main` 文件的头部注释中写元数据。所有字段可选，不需要独立 JSON。

```go
// distvis:name 我的协议
// distvis:description 选主与故障恢复实验。
// distvis:entry ./cmd/peer
// distvis:start node-1 Application.Ping {"peer":"node-2"} node-1 向 node-2 发起一次 Ping
package main
```

| 字段 | 作用 | 限制 |
| --- | --- | --- |
| `name` | 目录列表中的初始名称 | 非空，最多 100 个字符 |
| `description` | 初始代码的简介 | 非空，最多 1000 个字符 |
| `entry` | Go main 包的目录 | `.` 或项目内路径，例如 `./cmd/peer` |
| `start` | 节点启动后不会自动发消息时，说明从哪里开始：节点、应用输入方法、JSON 参数和一句说明 | 节点 `node-1`–`node-12`；方法为 `Service.Method` 或 gRPC 的 `/pkg.Service/Method`；参数为 JSON 对象；说明最多 300 个字符 |

实验运行后还没有任何消息时，界面按 `start` 提示起点，并可一键打开该节点的输入表单、填好参数；发送仍由你点击。会自动发消息的协议（选举、心跳、周期同步）不需要 `start`。

只解析 `package main` 前的注释，支持行注释和块注释；测试文件不参与。不要在多个文件重复声明同一字段。没有指定入口时，依次选择根目录 main 包、`./cmd/node`，或唯一的其他 main 包。多个候选入口不能判定时会报错。

旧 `distvis.json` 仍支持，Go 注释优先于同名 JSON 字段；编辑器的显式构建入口优先级最高。注释名称不会覆盖已命名的协议工作区。名称/简介注释不声明应用表单，表单来自 RPC 类型。

## Runtime 生命周期

```go
func Main(setup func(*Runtime) error)
func Serve(ctx context.Context, setup func(*Runtime) error) error
func (r *Runtime) OnStart(fn func(context.Context) error)
```

`Main` 从 coordinator 读取初始化，调用 setup，发布输入接口，启动 RPC 分发，执行 OnStart，处理退出信号并关闭连接。失败写到 stderr，进程以非零状态退出。`Serve` 是返回 error 的版本，适合测试和嵌入；同样需要 coordinator 的输入流。

setup 中初始化协议状态、取得客户端、注册服务和启动钩子。不要在 setup 中同步等待 RPC 返回，此时本节点的消息分发尚未启动。主动心跳、选举循环、初始消息放入 OnStart。

```go
node.OnStart(func(ctx context.Context) error {
    ticker := time.NewTicker(time.Second)
    defer ticker.Stop()
    for {
        select {
        case <-ctx.Done(): return ctx.Err()
        case <-ticker.C:
            // 执行一轮协议工作；自行决定是否重试失败 RPC。
        }
    }
})
```

多个钩子按注册顺序执行；前一个不返回，后一个不会开始。OnStart 保证本节点分发就绪，不保证其他节点已经完成启动。长任务应响应 `ctx.Done()`。setup 或钩子返回错误会终止节点；由生命周期取消引起的退出正常处理。

## 节点与连接

`Runtime` 嵌入 `*sdk.Node`，直接提供以下字段和方法。节点地址是 `node-1` 到 `node-N`，没有需要用户填写的 IP 或端口。

| API | 返回值 / 行为 |
| --- | --- |
| `node.ID` | 当前节点 ID，string |
| `node.Nodes` | 此实验的节点 ID 列表，`[]string`，包含自己；作为只读名单使用 |
| `node.Protocol` | 运行配置的协议标识；不能作为用户业务协议名称的可靠来源 |
| `node.Neighbor(offset int) string` | 按名单顺序环绕；1 为后继，-1 为前驱，0 为自己 |
| `node.RPC(peer string) (*rpc.Client, error)` | 获取并复用指定节点的标准 net/rpc 客户端；未知节点、已关闭 Runtime 会报错 |
| `node.RPCPeers() (map[string]*rpc.Client, error)` | 所有其他节点的客户端，不包含自己；map 可自行使用，客户端归 Runtime 所有 |
| `node.GRPCConn(peer string) (*grpc.ClientConn, error)` | 获取并复用 gRPC 连接 |
| `lab.GRPCPeers[T](node, constructor) (map[string]T, error)` | 用生成代码的 constructor 创建所有其他节点的 gRPC 客户端 |

```go
peers, err := node.RPCPeers()
// peers["node-2"].Call("Replica.Sync", args, &reply)

next, err := node.RPC(node.Neighbor(1))
// next.Call("Ring.Pass", token, &reply)

clients, err := lab.GRPCPeers(node, pb.NewReplicationClient)
// clients["node-2"].Apply(ctx, request)
```

节点名单由 coordinator 一次提供，连接由库管理，不需要在协议中遍历拨号或执行地址发现。Neighbor 不跳过故障节点；RPCPeers 包含故障节点，不代表健康检查。调用者不要 Close 这些共享客户端或连接。

故障恢复、令牌丢失处理、重试、成员变更和一致性规则仍由协议决定。取得客户端本身不会触发协议消息，也不保证目标服务已经注册。

## net/rpc 服务

```go
func (r *Runtime) Register(receiver any, options ...ServiceOption) error
func (r *Runtime) RegisterName(name string, receiver any, options ...ServiceOption) error
func Application() ServiceOption
```

遵循标准 net/rpc 方法约定：方法导出、两个参数、第二个参数为指针，返回一个 error；请求和响应类型须符合 net/rpc 的导出规则。JSON 标签影响浏览器字段名，节点间实际数据使用 gob。

```go
func (s *Replica) Sync(args SyncArgs, reply *SyncReply) error { /* ... */ }

// 内部服务：通信会记录，但不生成应用输入。
err := node.RegisterName("Replica", replica)
// 应用服务：该服务全部符合条件的导出方法生成输入表单。
err = node.RegisterName("Application", application, lab.Application())
```

必须在 setup 中注册。`Register` 默认以 receiver 的类型名作为服务名；`RegisterName` 显式指定名称。把内部方法和应用方法分开注册，可以只暴露希望学生调用的接口。

`client.Call`、`client.Go` 保留标准 API，可以并发调用。服务方法也可能并发执行，共享状态应加锁。net/rpc 方法没有 context；客户端超时不会强行停止已经执行的方法，因此超时不能等同于“对方没有执行”。

## gRPC 服务

```go
func (r *Runtime) GRPC(register func(grpc.ServiceRegistrar), applications ...string) error
```

使用 protoc 生成的正常注册函数和客户端；applications 填 protobuf 的完整服务名，内部服务省略标记。

```go
err := node.GRPC(func(server grpc.ServiceRegistrar) {
    pb.RegisterApplicationServer(server, service)
    pb.RegisterReplicationServer(server, service)
}, "lesson.Application")

clients, err := lab.GRPCPeers(node, pb.NewReplicationClient)
ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
defer cancel()
reply, err := clients["node-2"].Apply(ctx, &pb.NumberRequest{Number: 42})
```

只支持 unary RPC；注册带流式方法的服务会报错。保留 protobuf 数据、deadline/cancel、metadata、响应 header/trailer 和 gRPC status/details。调用选项目前处理 Header、Trailer；依赖压缩、负载均衡、TLS 连接属性或真实 HTTP/2 流控的实验不适用此受控传输。

Runtime.GRPC 创建默认 server。需要自定义 server/interceptor 时，使用后文的 Host API。protobuf descriptor 必须随生成代码注册，不需要额外开启服务端 reflection。平台不运行 protoc。

## 自动应用输入

学生看到的字段来自应用服务的请求类型，不需要额外维护 schema。普通 Go struct 字段可用 `json` 标签命名；未填写字段由 Go 的零值/JSON 解码规则处理，业务校验应在方法内部完成。

| 请求字段 | net/rpc 的输入 | gRPC 的输入 |
| --- | --- | --- |
| string | 文本 | 文本 |
| bool | 复选框；指针 bool 用 JSON | 无 presence 时复选框，有 presence 时 JSON |
| 小整数 / 浮点数 | 数字 | 数字 |
| 64 位整数 | 数字表单限制到 JavaScript 安全整数范围 | 十进制字符串，保留完整精度 |
| enum | 按 Go 底层类型处理 | 枚举选项名称 |
| bytes | Go JSON 表示，例如 []byte 为 base64 | base64 文本 |
| map / slice / 嵌套结构 | JSON | protobuf JSON |
| string 加标签 `distvis:"node"` | 节点下拉列表（单选，可不指定） | 不支持 |
| []string 加标签 `distvis:"nodes"` | 节点下拉列表（可多选） | 不支持 |

目标节点字段请加上述标签，例如 ``To []string `json:"to" distvis:"nodes"` ``。下拉列表列出本次运行中除当前节点外的节点，平台拒绝不存在或重复的节点。标签写在错误的类型上会被忽略，字段仍按原类型显示。

超过 16 个字段、嵌入字段、自定义 JSON 解码类型或无法安全映射的字段名，可能退化成一个必填 JSON 字段 `request`。protobuf well-known 类型也使用这个包装。具体 action 和字段以节点发布的 `input_schema` 事件为准，不要推测或写死自动生成的 action ID。

单次应用输入最多 8 KiB；没有隐式字符串转数字。应用调用及结果进入 `command` / `command_result` 事件。请求经应用接口进入本节点后，由业务方法决定是否再向其他节点发 RPC。

## 状态上报与持久化

```go
func (n *sdk.Node) Report(state any) error
func sdk.Save(value any) error
func sdk.Load(value any) error
```

Report 写入可 JSON 编码的状态，供节点详情和回放使用；每次应上报你希望看到的完整状态对象，不是增量补丁。它不会持久化业务状态，也不会向其他节点复制数据。

```go
if err := node.Report(map[string]any{
    "role": "follower", "term": term, "leader": leader,
}); err != nil { return err }
```

Save/Load 使用 `DISTVIS_STATE_DIR/state.json`，未设置目录时使用当前工作目录。容器中 `/state` 在同一次运行的节点崩溃与恢复之间保留；新运行不自动继承旧运行的状态。

```go
err := sdk.Load(&state)
if err != nil && !os.IsNotExist(err) { return err }
// 初次运行不存在状态文件是正常情况。
if err := sdk.Save(state); err != nil { return err }
```

Save 写临时文件、Sync 后重命名；协议应串行化持久状态更新，避免并发 Save 竞争同一临时文件。状态恢复后的角色、计时器和补偿动作由协议决定。

## 通信、时间与故障语义

全部库内 RPC 请求、响应和取消消息都经过 coordinator 的逻辑网络，进入可视化与日志。延迟和带宽按有向链路作用于编码消息，正反向分别排队。实验或链路可选指数分布抖动（在固定延迟上加 Exp(均值)）；默认每条有向链路 FIFO，抖动不会让消息乱序。实验的「丢包与乱序」（`network.lossRate`、`ordering: "unordered"`、`reorderRate`）可以随机丢弃消息或让后发的消息先到，见文末「网络策略」。外部自行创建的原生 TCP/HTTP/gRPC 连接不会被这套消息可视化自动捕获。

Runtime 使用默认 10 秒 RPC 超时，gRPC 调用者更短的 deadline 优先。丢包、链路中断或节点崩溃通常表现为超时/错误；不自动重试。受限并发为每个 Host 最多 128 个正在处理的入站/应用调用，超出会返回错误。

暂停、慢放和单步只改变浏览器的观察位置。后台节点继续运行，输入和故障总是作用于实时节点。真实 Go 程序使用系统调度与时钟；seed 决定内置模型和延迟抖动的随机序列，不保证真实程序重跑相同轨迹。

stdout 是平台 JSON 通信通道，不能使用 fmt.Println 输出普通日志。请用 `fmt.Fprintln(os.Stderr, ...)` 或默认 log 包写诊断。每个节点 stdout 总上限默认 4 MiB；避免高频、大体积状态上报。运行默认最多 10 分钟或60,000 个事件（limits.eventCount 可配置）。

## 低层 API：Host 与 Node

普通协议使用 Runtime 即可。需要自己管理生命周期或非 RPC 消息时，可以使用以下接口；不要同时让 Host 和你的代码消费同一个 Node.Receive / Node.Commands。

| API | 用途 |
| --- | --- |
| `sdk.Open() (*sdk.Node, error)` | 从 stdin/stdout 建立节点，需要 coordinator 初始化记录 |
| `sdk.OpenStreams(io.Reader, io.Writer) (*sdk.Node, error)` | 测试或嵌入时指定输入输出 |
| `node.Send(to string, payload any) error` | 发送 JSON 消息；返回表示写入传输，不表示目标已收到 |
| `node.Receive(ctx) (sdk.Message, error)` | 接收消息并记录 received；取消或 EOF 返回 error |
| `node.Commands` | `<-chan` 用法的 `chan sdk.Command`，接收应用输入 |
| `node.DeclareInput([]sdk.InputAction) error` | 非 RPC 协议声明应用接口，规则见平台 API 的输入 schema |
| `node.CommandResult(id string, result any, err error) error` | 上报应用调用结果 |
| `lab.New(node) *lab.Host` | 创建手动管理的 RPC Host，默认 Timeout 为 10 秒 |
| `host.Register / RegisterName` | 与 Runtime 的同名注册接口一致 |
| `host.ServeGRPC(server, applicationServices...) error` | 挂接已注册服务的自定义 gRPC server |
| `host.Dial(peer) *rpc.Client` | 创建标准 net/rpc 客户端；手动模式由调用者关闭 |
| `host.DialGRPC(peer) (*grpc.ClientConn, error)` | 创建生成客户端使用的连接；手动模式由调用者关闭 |
| `host.Run(ctx) error` | 发布 schema 并分发 RPC；只能调用一次 |
| `host.Close()` | 停止 Host 并清理其注册的服务资源 |

`Message` 包含 `ID`、`From`、`To` 和 `Payload json.RawMessage`。`Command` 包含 `ID`、`Action`、`Values json.RawMessage`，以及兼容字段 Key/Value。Host.Timeout 可在 Run 前配置；手动模式还需处理信号、连接关闭和启动顺序。

## 常见问题

| 现象 | 检查项 |
| --- | --- |
| go build 找不到 distvis | 容器由平台注入 replace；本机开发添加指向仓库的 replace，再运行 go mod tidy |
| 启动后没有应用输入 | 是否加了 Application 标记，或正确填写 protobuf 服务全名；节点是否已发布 input_schema |
| RPC 一直等到超时 | 服务名/方法名、目标 ID、节点状态、链路故障；是否误在 setup 中同步调用 RPC |
| JSON 输出格式错误 | 是否把普通日志写到了 stdout |
| 64 位数字精度丢失 | gRPC 使用字符串；net/rpc 数字表单限制为安全整数，必要时将业务字段定义为字符串 |
| 恢复后又产生一个令牌 | 初始令牌的创建需判断是否已有持久状态，不应每次启动都创建 |

自动化运行、故障注入和日志订阅见 [平台 HTTP API](/docs/platform)。

## 平台示例的教学范围

在“新建协议”中选择示例后，可以直接编辑其 Go 项目。

| 示例 | 可观察的行为 | 边界 |
| --- | --- | --- |
| Raft | 任期、投票、心跳、故障后的重选、向 Leader 转发提案 | 不含日志复制和多数派提交；Leader 接收提案不等于提交成功 |
| 令牌环 | 令牌传递、互斥访问、节点 payload 排队 | 不实现令牌丢失后的自动恢复 |
| LWW 存储 | 本地写入、传播、分区恢复后的收敛 | 最终一致性，不保证线性一致性 |
| gRPC 广播 | 标准 protobuf 客户端、应用输入、请求与响应记录 | 用于演示 unary RPC 接入，不提供共识保证 |

## 网络策略（SDK 实验）

所有 `distvis/sdk/rpc` 请求和回复都由 coordinator 投递。实验配置可以设置 `network` 和分别作用于 request/response 的 `networkProfiles`：

```json
{
  "network": {"lossRate": 0.1, "failureMode": "timeout", "ordering": "fifo"},
  "networkProfiles": {
    "response": {"lossRate": 0.2, "failureMode": "notify", "ordering": "unordered", "reorderRate": 0.5, "reorderMin": 40, "reorderMax": 180}
  }
}
```

`timeout` 让协议通过 RPC deadline 感知故障；`notify` 还会向 SDK 发布 `delivery_failed`，供需要区分立即投递失败和超时的测试使用。`failureMode` 只改变故障通知，不绕过协议 RPC。链路 `blocked`、节点崩溃、恢复和在途消息失效由同一个 coordinator 网络处理。`parameters` 是最多 8KiB 的协议参数，在初始化消息中提供给 Go 节点。
