// distvis:name Raft 选主 · net/rpc
// distvis:description 普通 RPC 实现任期、投票、心跳和提案路由；不包含日志复制或多数派提交。
package main

import lab "distvis/sdk/rpc"

func main() { lab.Main(configureRaft) }

func configureRaft(node *lab.Runtime) error {
	r, err := newRaft(node)
	if err != nil {
		return err
	}
	if err := node.RegisterName("Raft", r); err != nil {
		return err
	}
	if err := node.RegisterName("Application", &Application{r}, lab.Application()); err != nil {
		return err
	}
	node.OnStart(r.run)
	return r.report()
}
