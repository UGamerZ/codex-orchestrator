package broker

import (
	"fmt"
	"regexp"
	"strings"
)

// A conservative tripwire for newly added destructive SQL, not a complete code
// safety proof. It deliberately has no agent-controlled or generic-reply bypass.
var destructiveSQL = regexp.MustCompile(`(?is)\b(?:delete\s+from|truncate\s+(?:table\s+)?[a-z_` + "`" + `]|drop\s+(?:table|database|schema)\b|alter\s+table\b[^;]{0,500}\bdrop\s+(?:column\s+)?[a-z_` + "`" + `])`)

// A literal needle in this specific PHP comparison is inspected, not executed.
// Only mask the needle: SQL executed later on the same line must still be checked.
// Exclude interpolation, escapes and expressions so executable arguments cannot
// masquerade as literals. Do not exempt test files as a whole.
var phpSQLPrefixComparison = regexp.MustCompile(`^(\s*if\s*\(\s*\\?strpos\(\s*\$[a-zA-Z_][a-zA-Z_0-9]*\s*,\s*)('[^'\\\r\n]*'|"[^"$\\\r\n]*")(\s*\)\s*===\s*0\b)`)

func validateDataPreservationDiff(diff []byte) error {
	var added strings.Builder
	file := "patch"
	check := func() error {
		if destructiveSQL.MatchString(added.String()) {
			return fmt.Errorf("data-preservation guard: newly added destructive SQL in %s is forbidden; preserve source records and schema, and remove the destructive operation before retrying publication", file)
		}
		return nil
	}
	sqlFile := false
	for _, line := range strings.Split(string(diff), "\n") {
		if strings.HasPrefix(line, "+++ b/") {
			if err := check(); err != nil {
				return err
			}
			added.Reset()
			file = strings.TrimPrefix(line, "+++ b/")
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
		content := line[1:]
		if strings.HasSuffix(strings.ToLower(file), ".php") {
			content = phpSQLPrefixComparison.ReplaceAllString(content, "${1}''${3}")
		}
		added.WriteString(content)
		added.WriteByte('\n')
	}
	return check()
}
