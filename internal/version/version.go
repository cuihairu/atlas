// Package version provides build-time version information for Atlas.
package version

var (
	// Version is the semantic version of this build.
	Version = "v0.1.1"

	// GitCommit is the git commit hash, set at build time.
	GitCommit = "unknown"

	// BuildDate is the build timestamp, set at build time.
	BuildDate = "unknown"
)

// String returns a human-readable version string.
func String() string {
	return Version + " (" + GitCommit + ", " + BuildDate + ")"
}
