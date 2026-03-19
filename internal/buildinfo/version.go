package buildinfo

import "strings"

// Version is intentionally mutable via -ldflags -X.
var Version = "dev"

func CurrentVersion() string {
	value := strings.TrimSpace(Version)
	if value == "" {
		return "dev"
	}
	return value
}
