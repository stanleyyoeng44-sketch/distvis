// distvis:name LWW 副本存储 · net/rpc
// distvis:description 周期反熵复制键值；按逻辑版本与节点 ID 合并，分区恢复后最终收敛。
package main

import lab "distvis/sdk/rpc"

func main() { lab.Main(configureStore) }

func configureStore(node *lab.Runtime) error {
	store, err := newStore(node)
	if err != nil {
		return err
	}
	if err := node.RegisterName("Replica", store); err != nil {
		return err
	}
	if err := node.RegisterName("Application", &Application{store}, lab.Application()); err != nil {
		return err
	}
	node.OnStart(store.run)
	return store.report()
}
