package system

import "strings"

func modelBusy(diagnostic string) bool {
	d := strings.ToLower(diagnostic)
	return strings.Contains(d, "server_overloaded") || strings.Contains(d, "selected model is at capacity")
}
