package broker

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestDataPreservationGuard(t *testing.T) {
	for _, sql := range []string{"DELETE FROM settings", "delete\nfrom settings", "TRUNCATE TABLE settings", "TRUNCATE settings", "DROP TABLE settings", "DROP DATABASE app", "ALTER TABLE settings DROP COLUMN value"} {
		diff := "+" + sql
		if sql == "delete\nfrom settings" {
			diff = "+delete\n+from settings"
		}
		if validateDataPreservationDiff([]byte(diff)) == nil {
			t.Fatalf("allowed %s", sql)
		}
	}
	for _, diff := range []string{"+SELECT * FROM settings", "+INSERT INTO settings VALUES (1)", "-DELETE FROM settings\n+SELECT * FROM settings", "+CREATE TABLE settings (id INT)", "+++ b/DROP TABLE settings"} {
		if err := validateDataPreservationDiff([]byte(diff)); err != nil {
			t.Fatal(err)
		}
	}
}

func TestSQLCommentDoesNotBlockPublication(t *testing.T) {
	diff := "+++ b/tests/_data/schema.sql\n+-- executing their DELETE/TRUNCATE statements against acceptance fixture rows.\n+SELECT 1;"
	if err := validateDataPreservationDiff([]byte(diff)); err != nil {
		t.Fatal(err)
	}
	if validateDataPreservationDiff([]byte(diff+"\n+DELETE FROM settings;")) == nil {
		t.Fatal("actual deletion allowed")
	}
}

func TestSQLInspectionDoesNotBlockPublication(t *testing.T) {
	comparison := `if (strpos($query, 'DELETE FROM s_products_tags') === 0 && count($args) > 0) {`
	for _, tc := range []struct {
		name, file, code string
		blocked          bool
	}{
		{"regression assertion", "tests/unit/RecalcTagsTest.php", comparison, false},
		{"production inspection", "app.php", comparison, false},
		{"actual execution in tests", "tests/unit/RecalcTagsTest.php", `$db->query('DELETE FROM settings');`, true},
		{"execution after inspection", "app.php", comparison + `$db->query('DROP TABLE settings');`, true},
		{"executable argument", "app.php", `if (strpos($query, $db->query('DELETE FROM settings')) === 0) {`, true},
		{"not PHP", "migration.sql", comparison, true},
		{"SQL string containing comparison", "app.php", `$db->query("if (strpos($query, 'DELETE FROM settings') === 0)");`, true},
		{"interpolated needle", "app.php", `if (strpos($query, "DELETE FROM $table") === 0) {`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := validateDataPreservationDiff([]byte("+++ b/" + tc.file + "\n+" + tc.code))
			if (err != nil) != tc.blocked {
				t.Fatalf("blocked=%v: %v", tc.blocked, err)
			}
			if err != nil && !strings.Contains(err.Error(), tc.file) {
				t.Fatalf("missing affected file: %v", err)
			}
		})
	}
}

func TestPublicationGuardIgnoresOnlyLineEndingChanges(t *testing.T) {
	for _, destructive := range []bool{false, true} {
		t.Run(map[bool]string{false: "existing-sql", true: "new-destructive-sql"}[destructive], func(t *testing.T) {
			git := func(dir string, args ...string) {
				t.Helper()
				cmd := exec.Command("git", append([]string{"-c", "core.autocrlf=false"}, args...)...)
				cmd.Dir = dir
				if b, err := cmd.CombinedOutput(); err != nil {
					t.Fatalf("git %v: %v %s", args, err, b)
				}
			}
			source := t.TempDir()
			git(source, "init", "-b", "dev")
			git(source, "config", "user.name", "Test")
			git(source, "config", "user.email", "test@example.com")
			base := "<?php\n$db->query('DELETE FROM expired_sessions WHERE expires < ?');\n$enabled = false;\n"
			if err := os.WriteFile(filepath.Join(source, "app.php"), []byte(base), 0600); err != nil {
				t.Fatal(err)
			}
			git(source, "add", "--all")
			git(source, "commit", "-m", "Baseline")
			bundlePath := filepath.Join(t.TempDir(), "source.bundle")
			git(source, "bundle", "create", bundlePath, "--all")
			bundle, err := os.ReadFile(bundlePath)
			if err != nil {
				t.Fatal(err)
			}
			root := t.TempDir()
			p := Paths{filepath.Join(root, "workspace"), filepath.Join(root, "publish"), filepath.Join(root, "review")}
			req := Request{Operation: "prepare", Local: true, Bundle: bundle, Base: "dev", Branch: "codex/line-endings"}
			baseline, err := Execute(context.Background(), req, p)
			if err != nil {
				t.Fatal(err)
			}
			updated := strings.ReplaceAll(base, "false", "true")
			if destructive {
				updated += "$db->query('DROP TABLE settings');\n"
			}
			updated = strings.ReplaceAll(updated, "\n", "\r\n")
			if err := os.WriteFile(filepath.Join(p.Workspace, "app.php"), []byte(updated), 0600); err != nil {
				t.Fatal(err)
			}
			req.Operation = "publish"
			req.SHA = baseline.SHA
			req.Message = "Enable application feature"
			_, err = Execute(context.Background(), req, p)
			if destructive {
				if err == nil || !strings.Contains(err.Error(), "data-preservation guard:") {
					t.Fatalf("new destructive SQL not blocked: %v", err)
				}
			} else if err != nil {
				t.Fatalf("line endings blocked unchanged SQL: %v", err)
			}
		})
	}
}
