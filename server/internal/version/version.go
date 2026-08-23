// Package version holds the build label this binary reports (#256,
// ADR-0072). A dependency-free leaf on purpose: anything may read it
// without risking an import cycle.
package version

// Version is the opaque label identifying this build, injected at link
// time with:
//
//	-ldflags "-X github.com/XiovV/calich/server/internal/version.Version=v1.2.3"
//
// It is never parsed, compared, or normalised — whoever writes the release
// tag decides what it says, and it round-trips verbatim to the client.
//
// "dev" is what an uninjected build reports, which is every `go run` and
// every `make build-backend` without VERSION set. A release build whose
// ldflag failed to fire is therefore indistinguishable from a laptop; only
// the release pipeline asserting the flag can close that gap (ADR-0072).
var Version = "dev"
