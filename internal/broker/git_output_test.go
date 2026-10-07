package broker

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestGitDiagnosticRedactsCredentials(t *testing.T) {
	token := "example-secret-token"
	raw := "remote failed " + token + " Basic " + base64.StdEncoding.EncodeToString([]byte("x-access-token:"+token))
	got := safeGitDiagnostic(raw, token)
	if strings.Contains(got, token) || strings.Contains(got, base64.StdEncoding.EncodeToString([]byte("x-access-token:"+token))) {
		t.Fatal("credential leaked")
	}
	if !strings.Contains(got, "remote failed") {
		t.Fatal("diagnostic lost")
	}
	if len([]rune(safeGitDiagnostic(strings.Repeat("я", 2000), ""))) > 1003 {
		t.Fatal("diagnostic unbounded")
	}
}
