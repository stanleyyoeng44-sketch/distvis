// A module is Go's unit of dependency management; this local name need not be a website.
module lesson/cristian

// Minimum language/toolchain version, not the version of this lesson.
go 1.24

// v0.0.0 is a placeholder: replace below resolves the SDK to this checkout.
require distvis v0.0.0

require (
	golang.org/x/net v0.34.0 // indirect
	golang.org/x/sys v0.29.0 // indirect
	golang.org/x/text v0.21.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20250115164207-1a7da9e5054f // indirect
	google.golang.org/grpc v1.71.1 // indirect
	google.golang.org/protobuf v1.36.6 // indirect
)

// From protocols/cristian, ../.. is the DistVis repository. Container builds
// replace this with /opt/distvis automatically; no absolute personal path is needed.
replace distvis => ../..
