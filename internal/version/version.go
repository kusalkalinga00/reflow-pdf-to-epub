// Package version reports build metadata for the reflow binary.
package version

import (
	"fmt"
	"runtime"
	"runtime/debug"
)

// Set at link time:
//
//	go build -ldflags "-X github.com/kusalkalinga00/tool-factory-epub-convertor/internal/version.Version=v0.1.0"
//
// Commit and BuildDate are optional; when Commit is empty the revision is read
// from the embedded build info, which Go fills in for builds from a git tree.
var (
	Version   = "dev"
	Commit    = ""
	BuildDate = ""
)

// String returns a single-line description of the build, suitable for logs and
// bug reports.
func String() string {
	v := Version
	if v == "" {
		v = "dev"
	}

	out := fmt.Sprintf("reflow %s (commit %s", v, commit())
	if BuildDate != "" {
		out += ", built " + BuildDate
	}
	return fmt.Sprintf("%s) %s/%s %s", out, runtime.GOOS, runtime.GOARCH, runtime.Version())
}

// commit prefers the link-time value, then falls back to the VCS revision that
// the Go toolchain embeds.
func commit() string {
	if Commit != "" {
		return Commit
	}

	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "unknown"
	}
	for _, setting := range info.Settings {
		if setting.Key == "vcs.revision" && setting.Value != "" {
			if len(setting.Value) > 7 {
				return setting.Value[:7]
			}
			return setting.Value
		}
	}
	return "unknown"
}
