// Each lesson is an independent module, so it can be imported into DistVis alone.
module lesson/ntp

// The SDK requires Go 1.24 or newer.
go 1.24

// The local SDK is not downloaded from a registry at this placeholder version.
require distvis v0.0.0

require (
	golang.org/x/net v0.34.0 // indirect
	golang.org/x/sys v0.29.0 // indirect
	golang.org/x/text v0.21.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20250115164207-1a7da9e5054f // indirect
	google.golang.org/grpc v1.71.1 // indirect
	google.golang.org/protobuf v1.36.6 // indirect
)

// Local IDE/tests use the repository two directories above this module.
// DistVis rewrites this replacement for its container build environment.
replace distvis => ../..
