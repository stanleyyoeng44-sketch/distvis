// distvis:name Lamport and Vector Clocks
// distvis:description Timestamp the same local/send/receive events with Lamport, total-order and vector clocks; distinguish causality from concurrency.
// distvis:start node-1 Application.Local {"label":"a"} Create event a, then follow the six-event walkthrough in README.md.

// All Go files here share one package. A main package needs a main function;
// Go calls that function after package initialization when the executable starts.
package main

// Alias the DistVis runtime as lab so it is visibly different from net/rpc,
// the standard library package whose Client we use for peer calls.
import lab "distvis/sdk/rpc"

func main() {
	// Passing configure (no parentheses) passes a function value. Main calls it
	// once membership is available and then runs the node until shutdown.
	// Setup only registers services; user input creates the lesson's events.
	lab.Main(configure)
}
