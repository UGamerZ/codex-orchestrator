package broker

import "testing"

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
