// Package observability holds what operators use to see inside the service:
// build metadata now, the metrics registry and collectors later.
package observability

import "runtime"

// BuildInfo describes the running binary. Version, commit and build time are
// injected by the linker (see LDFLAGS in the Makefile) and exposed via
// GET /version, the -version flag and the uptime_build_info metric.
type BuildInfo struct {
	Version   string
	Commit    string
	BuiltAt   string
	GoVersion string
}

// NewBuildInfo returns BuildInfo for the current binary.
func NewBuildInfo(version, commit, builtAt string) BuildInfo {
	return BuildInfo{
		Version:   version,
		Commit:    commit,
		BuiltAt:   builtAt,
		GoVersion: runtime.Version(),
	}
}
