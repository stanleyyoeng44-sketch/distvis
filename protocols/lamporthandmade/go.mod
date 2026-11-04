// Modules group packages and dependencies; they are not the same as packages.
module lesson/logical-clocks

// The examples and SDK use language/library features available since Go 1.24.
go 1.24

require distvis v0.0.0 // A local SDK dependency, not a published version to fetch.

require (
	golang.org/x/net v0.34.0 // indirect
	golang.org/x/sys v0.29.0 // indirect
	golang.org/x/text v0.21.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20250115164207-1a7da9e5054f // indirect
	google.golang.org/grpc v1.71.1 // indirect
	google.golang.org/protobuf v1.36.6 // indirect
)

// Resolve distvis/sdk and distvis/sdk/rpc from this repository for local builds.
// DistVis supplies its own replacement path when compiling the imported snapshot.
replace distvis => ../..
