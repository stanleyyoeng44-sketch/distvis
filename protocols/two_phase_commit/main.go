// distvis:name Two-Phase Commit
// distvis:description Bank transfer with a coordinator (node-1 = TM) and resource managers (node-2 ... node-N = RMs), each holding 100. The TM sends Prepare to every RM, commits only on a YES from everyone (one NO aborts), writes commit/abort to its log before notifying, and after a crash resumes from that log: no decision is presumed abort, a commit decision is resent until every RM acknowledges.
// distvis:start node-1 Application.Transfer {"from":"node-2","to":"node-3","amount":30} Transfer 30 from node-2 to node-3.
package main

import (
	"context"
	"os"

	"distvis/sdk"
	lab "distvis/sdk/rpc"
)

func main() { lab.Main(configure) }

func configure(node *lab.Runtime) error {
	peers, err := node.RPCPeers()
	if err != nil {
		return err
	}
	if node.ID == coordinatorNode {
		coordinator, err := newCoordinator(node.ID, node.Nodes, peers, deps{sdk.Save, sdk.Load, node.Report})
		if err != nil {
			return err
		}
		if err := node.RegisterName("TM", coordinator); err != nil {
			return err
		}
		if err := node.RegisterName("Application", &Application{coordinator: coordinator}, lab.Application()); err != nil {
			return err
		}
		node.OnStart(func(ctx context.Context) error {
			coordinator.startRecovery(ctx)
			return nil
		})
		return coordinator.publish()
	}

	rm, err := newResourceManager(node.ID, peers, deps{sdk.Save, sdk.Load, node.Report})
	if err != nil {
		return err
	}
	if err := node.RegisterName("RM", rm); err != nil {
		return err
	}
	if err := node.RegisterName("Application", &Application{participant: rm}, lab.Application()); err != nil {
		return err
	}
	node.OnStart(func(ctx context.Context) error {
		rm.startRecovery(ctx)
		return nil
	})
	return rm.publish()
}

type deps struct {
	save   func(any) error
	load   func(any) error
	report func(any) error
}

// loadPersisted ignores a missing state file and returns other errors.
func loadPersisted(load func(any) error, target any) error {
	if err := load(target); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
