// distvis:name Primary-Backup Replication · net/rpc
// distvis:description node-1 is the fixed primary and every other node is a backup; writes sent to any node are forwarded to the primary, which assigns increasing log indexes. Supports async/sync replication and local/primary/session reads. There is no automatic failover or log repair, so a crashed primary stays unavailable.
// distvis:start node-1 Application.Mode {"replication":"async","replicationDelayMs":4000} Set async replication with a 4s delay, then Put/Get from any node.
package main

import lab "distvis/sdk/rpc"

func main() { lab.Main(configure) }

func configure(node *lab.Runtime) error {
	server, err := newServer(node)
	if err != nil {
		return err
	}
	if err := node.RegisterName("Replica", server); err != nil {
		return err
	}
	if err := node.RegisterName("Application", &Application{server: server}, lab.Application()); err != nil {
		return err
	}
	return server.report()
}
