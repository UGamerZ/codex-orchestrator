package system

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

func pullRequestTitle(title string) string {
	title = strings.Join(strings.Fields(title), " ")
	chars := []rune(title)
	if len(chars) > 120 {
		return string(chars[:119]) + "…"
	}
	return title
}

var githubHTTPStatus = regexp.MustCompile(`\(HTTP ([0-9]{3})\)`)

// Only emit known diagnostic labels/status, never raw CLI output or API messages
// that could echo credentials, private content, or command arguments.
func githubOperationError(body, stderr []byte) error {
	var response struct {
		Status string `json:"status"`
		Errors []struct {
			Field   string `json:"field"`
			Message string `json:"message"`
		} `json:"errors"`
	}
	if json.Unmarshal(body, &response) == nil {
		for _, detail := range response.Errors {
			if response.Status == "422" && detail.Field == "title" && strings.Contains(strings.ToLower(detail.Message), "title is too long") {
				return fmt.Errorf("GitHub rejected request (HTTP 422): PR title exceeds the 256-character limit")
			}
		}
	}
	if status := githubHTTPStatus.FindSubmatch(stderr); len(status) == 2 {
		return fmt.Errorf("GitHub operation failed (HTTP %s); check request validation, account permissions or API availability", status[1])
	}
	return fmt.Errorf("GitHub operation failed (check account access, connectivity or limits)")
}
