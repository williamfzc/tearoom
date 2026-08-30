// Package version owns build metadata injected into the Tearoom binary.
// The defaults identify an ordinary local development build.
package version

// Version is the semantic version supplied by the release build, or "dev"
// when no release metadata was injected.
var Version = "dev"

// Commit is the source revision supplied by the release build, or "unknown"
// when no revision metadata was injected.
var Commit = "unknown"
