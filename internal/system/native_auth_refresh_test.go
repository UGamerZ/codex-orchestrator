package system

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestNativeAuthRefreshPreservesNewestLoginForSameAccount(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source.json")
	home := filepath.Join(root, "runtime")
	os.MkdirAll(home, 0700)
	runtimePath := filepath.Join(home, "auth.json")
	write := func(path, account, refresh, access string) {
		t.Helper()
		if err := writePrivateJSON(path, map[string]any{"auth_mode": "chatgpt", "tokens": map[string]string{"account_id": account, "access_token": access}, "last_refresh": refresh, "unrelated_secret": "must-not-copy"}); err != nil {
			t.Fatal(err)
		}
	}
	readAccess := func() string {
		t.Helper()
		b, err := os.ReadFile(runtimePath)
		if err != nil {
			t.Fatal(err)
		}
		var auth struct {
			Tokens map[string]string `json:"tokens"`
		}
		if err = json.Unmarshal(b, &auth); err != nil {
			t.Fatal(err)
		}
		return auth.Tokens["access_token"]
	}
	write(source, "account-one", "2026-10-04T03:00:00Z", "old-source")
	write(runtimePath, "account-one", "2026-10-14T03:00:00Z", "refreshed-runtime")
	if err := prepareNativeAuth(source, home); err != nil {
		t.Fatal(err)
	}
	if readAccess() != "refreshed-runtime" {
		t.Fatal("fresh cache overwritten by stale source")
	}
	write(source, "account-one", "2026-10-15T03:00:00Z", "new-host-login")
	if err := prepareNativeAuth(source, home); err != nil {
		t.Fatal(err)
	}
	if readAccess() != "new-host-login" {
		t.Fatal("new source login was not propagated")
	}
	b, _ := os.ReadFile(runtimePath)
	var clean map[string]any
	json.Unmarshal(b, &clean)
	if _, ok := clean["unrelated_secret"]; ok {
		t.Fatal("unrelated credentials copied")
	}
	write(source, "account-two", "2026-10-01T03:00:00Z", "explicit-account-change")
	if err := prepareNativeAuth(source, home); err != nil {
		t.Fatal(err)
	}
	if readAccess() != "explicit-account-change" {
		t.Fatal("configured account change ignored")
	}
}
