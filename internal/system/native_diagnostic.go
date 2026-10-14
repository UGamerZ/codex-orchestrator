package system

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Only terminal error events and stderr error lines are eligible. Never retain
// stdout messages, tool output, or reasoning as a process failure diagnostic.
func nativeProcessError(stdout, stderr string, exitErr error, secrets []string) error {
	detail := ""
	for _, line := range strings.Split(stdout, "\n") {
		var event struct {
			Type    string `json:"type"`
			Message string `json:"message"`
			Error   struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if json.Unmarshal([]byte(line), &event) != nil {
			continue
		}
		if event.Type == "error" {
			detail = event.Message
		}
		if event.Type == "turn.failed" && event.Error.Message != "" {
			detail = event.Error.Message
		}
	}
	if detail == "" {
		for _, line := range strings.Split(stderr, "\n") {
			lower := strings.ToLower(strings.TrimSpace(line))
			if strings.HasPrefix(lower, "error:") || strings.HasPrefix(lower, "error ") {
				detail = strings.TrimSpace(line)
			}
		}
	}
	message := fmt.Sprintf("native agent process failed (%v); sandbox restrictions retained", exitErr)
	if detail != "" {
		message += ": " + clip(scrubAgentLog(detail, secrets), 1600)
	}
	return fmt.Errorf("%s", message)
}
