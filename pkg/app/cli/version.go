package cli

import "strings"

var (
	Version = "dev"
	Commit  = ""
	Date    = ""
)

func versionString() string {
	parts := []string{Version}
	if Commit != "" {
		parts = append(parts, "commit "+Commit)
	}
	if Date != "" {
		parts = append(parts, "built "+Date)
	}
	return strings.Join(parts, ", ")
}
