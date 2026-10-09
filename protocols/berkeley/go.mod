// The module name identifies this lesson; package main inside it is an executable.
module lesson/berkeley

// This is the minimum Go version used by the DistVis SDK.
go 1.24

require distvis v0.0.0 // Placeholder version resolved by the local replacement.

require (
	golang.org/x/net v0.34.0 // indirect
	golang.org/x/sys v0.29.0 // indirect
	golang.org/x/text v0.21.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20250115164207-1a7da9e5054f // indirect
	google.golang.org/grpc v1.71.1 // indirect
	google.golang.org/protobuf v1.36.6 // indirect
)

// A relative path keeps the checkout movable. DistVis overrides it in Docker.
replace distvis => ../..
