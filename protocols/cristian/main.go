// distvis:name Cristian Clock Synchronization
// distvis:description Estimate server time with RTT/2; explore clock skew, drift and uncertainty using adjustable software clocks.
// distvis:start node-2 Application.Sync {"minDelayMS":0} Synchronize node-2 with the reference clock on node-1.

// Every .go file in this directory belongs to package main. Go compiles them
// together; their order and filenames do not determine execution order.
// A package named main builds an executable, whose entry point is func main().
package main

// An import alias gives a package a convenient local name. "lab" is our name
// for DistVis's RPC runtime, not a second package or a language keyword.
import lab "distvis/sdk/rpc"

func main() {
	// Pass the FUNCTION configure, rather than calling configure() ourselves.
	// Main reads membership from DistVis, calls configure, starts RPC dispatch,
	// handles shutdown signals, and closes the connections that it owns.
	// Running this executable without a coordinator is not a standalone demo:
	// the runtime expects the coordinator's initialization stream on stdin.
	lab.Main(configure)
}
