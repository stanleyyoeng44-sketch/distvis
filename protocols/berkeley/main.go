// distvis:name Berkeley Clock Synchronization
// distvis:description A fixed coordinator polls software clocks, rejects outliers, averages offsets and sends deltas for gradual adjustment.
// distvis:start node-1 Application.Sync {"outlierThresholdMS":2000} Poll the group and gently move all reachable, stable clocks toward their filtered mean.

// package main tells Go to build an executable rather than an importable library.
// main.go is deliberately small: start here, then read clock.go and protocol.go.
package main

// This is a package alias. The standard net/rpc API is used in protocol.go;
// lab supplies membership, transport integration and lifecycle management.
import lab "distvis/sdk/rpc"

func main() {
	// Main invokes configure with this node's Runtime. It also owns stdin/stdout,
	// RPC dispatch, signal handling and shared connection cleanup. Never print
	// ordinary text to stdout: DistVis uses it as a machine-readable channel.
	lab.Main(configure)
}
