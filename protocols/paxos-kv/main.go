// distvis:name Paxos Replicated State Machine KV
// distvis:description Durable per-slot Paxos; Put and Get execute in log order, competing commands retry and recovered nodes fill holes through Paxos.
// distvis:start node-1 Application.Put {"key":"x","value":"1"} Commit a write through a majority.
package main

import (
	"distvis/sdk"
	lab "distvis/sdk/rpc"
	"net/rpc"
	"os"
	"sync"
)

// main hands startup, incoming RPCs [requests to call methods on this node],
// shutdown, and connection cleanup to DistVis. configure assembles our protocol;
// the actual Paxos and key-value operations are implemented in protocol.go.
func main() { lab.Main(configure) }

// Server holds all three Paxos roles on this node:
//   - proposer: tries to get an operation chosen for a log position;
//   - acceptor: records promises and accepts proposals from any proposer;
//   - learner: records chosen operations and executes them in log order.
//
// There is no elected leader here. Different nodes can propose concurrently,
// and each log position runs its own prepare and accept phases.
type Server struct {
	// mu protects shared protocol state. Incoming RPC handlers, application
	// calls, and hole-recovery goroutines [concurrently running Go functions]
	// can all access that state. Methods ending in Locked expect mu to be held,
	// except during configure, before concurrent request processing starts.
	mu sync.Mutex
	// operation allows only one application Put/Get at a time on this node.
	// It does not block other nodes' proposals or this node's Paxos handlers.
	// Keeping this separate from mu lets us wait for network replies without
	// holding the lock those handlers need to produce replies.
	operation sync.Mutex
	id        string
	// index is this node's stable, one-based position in the membership list.
	// choose uses it to distinguish ballots [ordered proposal identifiers]
	// generated concurrently by different nodes.
	index uint64
	// members includes this node; peers contains clients for the other nodes.
	// Majority sizes always use the full membership, not just reachable peers.
	members []string
	peers   map[string]*rpc.Client
	// disk contains the state saved across crashes: promises, accepted values,
	// decisions, operation IDs, and the key-value store derived from the log.
	disk Disk
	// These functions are provided by DistVis in configure. Tests can replace
	// them to observe saved state or simulate a storage failure.
	save   func(any) error
	report func(any) error
	// storageError remembers the first failed save. Subsequent persistence
	// attempts fail too, rather than acknowledging state that may be lost.
	storageError string
	// phase, last, and counters are local display/diagnostic information, not
	// votes and not evidence that another node has learned a decision.
	phase               string
	last                *Result
	conflicts, messages uint64
	// amnesia is an intentionally unsafe teaching option that skips saves.
	// It does not immediately clear memory and is not itself saved to disk.
	amnesia bool
	// catching tracks log positions currently being recovered on this node,
	// so repeated Prepare calls do not start duplicate recovery tasks for one
	// position. The entries are removed when those tasks finish.
	catching map[int]bool
}

// Application exposes user-facing methods separately from the internal Paxos
// methods. Both services use the same Server, so they share the same state.
type Application struct{ s *Server }

// configure connects the protocol to DistVis without starting a proposal.
// Recovery first restores durable state [state saved across a node restart].
// Missing log decisions are recovered later when Prepare observes a later slot;
// there is no startup scan or periodic recovery loop in this implementation.
func configure(node *lab.Runtime) error {
	// Getting clients does not send a prepare and does not prove peers are
	// healthy. DistVis owns these connections and closes them at shutdown.
	peers, err := node.RPCPeers()
	if err != nil {
		return err
	}
	// Empty maps make the first run usable before any state file exists.
	s := &Server{id: node.ID, members: node.Nodes, peers: peers, save: sdk.Save, report: node.Report, phase: "idle", catching: map[int]bool{}, disk: Disk{Slots: map[int]Slot{}, Values: map[string]string{}, Results: map[string]Result{}}}
	for i, id := range node.Nodes {
		if id == node.ID {
			s.index = uint64(i + 1)
		}
	}
	// A missing file means a fresh node. Any other load error stops startup:
	// silently forgetting an existing promise could allow conflicting votes.
	if err = sdk.Load(&s.disk); err != nil && !os.IsNotExist(err) {
		return err
	}
	// Internal RPCs are visible as protocol messages, not application inputs.
	if err = node.RegisterName("Paxos", s); err != nil {
		return err
	}
	// lab.Application marks Put, Get, Read, and Amnesia as user-callable inputs.
	if err = node.RegisterName("Application", &Application{s}, lab.Application()); err != nil {
		return err
	}
	// Setup is still single-threaded, so there is no concurrent state access
	// here. Reporting displays the restored state; it does not persist it.
	return s.publishLocked()
}
