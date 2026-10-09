// distvis:name NTP Clock Synchronization (Teaching Model)
// distvis:description Four timestamps, processing-time subtraction, minimum-delay filtering and a loop-free time-server hierarchy. Not wire-compatible NTP.
// distvis:start node-2 Application.Sync {"servers":["node-1"],"samples":3,"processingMS":100} First synchronize node-2; then node-3 may use node-2 as its upstream server.

// main is an executable package. Its other files contribute types and functions
// to this same package without needing imports between those files.
package main

// The alias avoids confusing DistVis's lifecycle package with the standard
// library's net/rpc package (which supplies the actual Client type).
import lab "distvis/sdk/rpc"

func main() {
	// A function is a value in Go: configure is a callback passed to Main.
	// The runtime supplies membership, registers services via that callback,
	// starts dispatch, and eventually handles signals and connection cleanup.
	// We do not start containers or open our own TCP ports in a protocol.
	lab.Main(configure)
}
