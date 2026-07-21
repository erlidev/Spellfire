// Package build exposes information about how the running binary was compiled,
// so clients can tell whether a deployment is recent.
package build

import (
	"runtime/debug"
)

// Injected at link time via:
//
//	-ldflags "-X spellfire/server/internal/build.Time=<RFC3339> -X spellfire/server/internal/build.Commit=<sha>"
//
// When unset (e.g. `go run` in a checkout), Get falls back to the VCS stamp Go
// embeds automatically.
var (
	Time   = ""
	Commit = ""
)

// Info is the JSON-serialisable build description returned by GET /api/version.
type Info struct {
	Time   string `json:"time"`   // RFC3339 UTC build timestamp, or "" if unknown.
	Commit string `json:"commit"` // Git revision, or "" if unknown.
}

// Get resolves the build info, preferring link-time values and falling back to
// Go's embedded VCS metadata for local (non-ldflags) builds.
func Get() Info {
	info := Info{Time: Time, Commit: Commit}
	if info.Time != "" && info.Commit != "" {
		return info
	}
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return info
	}
	for _, s := range bi.Settings {
		switch s.Key {
		case "vcs.revision":
			if info.Commit == "" {
				info.Commit = s.Value
			}
		case "vcs.time":
			if info.Time == "" {
				info.Time = s.Value
			}
		}
	}
	return info
}
