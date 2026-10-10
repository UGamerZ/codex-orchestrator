package broker

import (
	"fmt"
	"regexp"
	"strings"
)

// A conservative tripwire for newly added destructive SQL, not a complete code
// safety proof. It deliberately has no agent-controlled or generic-reply bypass.
var destructiveSQL = regexp.MustCompile(`(?is)\b(?:delete\s+from|truncate\s+(?:table\s+)?[a-z_` + "`" + `]|drop\s+(?:table|database|schema)\b|alter\s+table\b[^;]{0,500}\bdrop\s+(?:column\s+)?[a-z_` + "`" + `])`)

func validateDataPreservationDiff(diff []byte) error {
	var added strings.Builder
	sqlFile := false
	for _, line := range strings.Split(string(diff), "\n") {
		if strings.HasPrefix(line, "+++ b/") {
			sqlFile = strings.HasSuffix(strings.ToLower(line), ".sql")
			continue
		}
		if strings.HasPrefix(line, "+++") || !strings.HasPrefix(line, "+") {
			continue
		}
		if sqlFile {
			trimmed := strings.TrimSpace(line[1:])
			if strings.HasPrefix(trimmed, "--") || strings.HasPrefix(trimmed, "#") {
				continue
			}
		}
		added.WriteString(line[1:])
		added.WriteByte('\n')
	}
	if destructiveSQL.MatchString(added.String()) {
		return fmt.Errorf("data-preservation guard: newly added destructive SQL is forbidden; preserve source records and schema, and remove the destructive operation before retrying publication")
	}
	return nil
}
