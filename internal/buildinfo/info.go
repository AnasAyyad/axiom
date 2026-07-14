package buildinfo

import (
	"runtime"
	"strings"
)

// Linker-injected values use safe development defaults for local builds.
var (
	Version = "dev"
	Commit  = "unknown"
	BuiltAt = "unknown"
	Dirty   = "true"
)

// Info is the immutable, non-secret identity of the running binary.
type Info struct {
	Version   string `json:"version"`
	Commit    string `json:"commit"`
	BuiltAt   string `json:"built_at"`
	GoVersion string `json:"go_version"`
	Dirty     bool   `json:"dirty"`
}

// Current returns normalized build identity without reading runtime config.
func Current() Info {
	return Info{
		Version:   normalized(Version),
		Commit:    normalized(Commit),
		BuiltAt:   normalized(BuiltAt),
		GoVersion: runtime.Version(),
		Dirty:     strings.EqualFold(Dirty, "true"),
	}
}

func normalized(value string) string {
	if strings.TrimSpace(value) == "" {
		return "unknown"
	}
	return value
}
