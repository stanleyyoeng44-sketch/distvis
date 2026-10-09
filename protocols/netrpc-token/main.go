// distvis:name Token Ring · net/rpc
// distvis:description 普通 Go RPC 服务：应用提交 payload，持有令牌时处理，使用 client.Call 传给下一个节点。
package main

import lab "distvis/sdk/rpc"

func main() { lab.Main(configureRing) }

func configureRing(node *lab.Runtime) error {
	ring, err := newRing(node)
	if err != nil {
		return err
	}
	if err := node.RegisterName("Ring", ring); err != nil {
		return err
	}
	if err := node.RegisterName("Application", &Application{ring}, lab.Application()); err != nil {
		return err
	}
	node.OnStart(ring.run)
	return nil
}
