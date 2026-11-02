package system

import (
	"context"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestCreatePRLimitsTitleAndPreservesOriginal(t *testing.T) {
	for _, original := range []string{"Normal task title", strings.Repeat("я", 120), strings.Repeat("я", 121), strings.Repeat("я", 767), "Multi\nline\ttitle"} {
		c := fixture(t)
		j := &Job{Title: original, Branch: "codex/task", Summary: "Summary"}
		created := 0
		c.API.ghTransport = func(_ context.Context, args ...string) ([]byte, error) {
			if !strings.Contains(strings.Join(args, " "), "POST") {
				return []byte(`[]`), nil
			}
			created++
			var title, body string
			for _, arg := range args {
				if strings.HasPrefix(arg, "title=") {
					title = strings.TrimPrefix(arg, "title=")
				}
				if strings.HasPrefix(arg, "body=") {
					body = strings.TrimPrefix(arg, "body=")
				}
			}
			if utf8.RuneCountInString(title) > 120 || !utf8.ValidString(title) || strings.ContainsAny(title, "\r\n\t") {
				t.Fatalf("invalid title: %q", title)
			}
			if title != original && !strings.Contains(body, original) {
				t.Fatal("full task text lost")
			}
			return []byte(`{"number":7,"html_url":"https://example.org/pr/7"}`), nil
		}
		if _, _, err := c.API.CreatePR(context.Background(), j); err != nil {
			t.Fatal(err)
		}
		if j.Title != original || created != 1 {
			t.Fatal("task title changed or duplicate PR created")
		}
	}
}

func TestGitHubDiagnosticDoesNotExposeRawOutput(t *testing.T) {
	body := []byte(`{"status":"422","errors":[{"field":"title","message":"title is too long (maximum is 256 characters) secret-token"}]}`)
	diagnostic := githubOperationError(body, []byte("gh: secret-token (HTTP 422)")).Error()
	if !strings.Contains(diagnostic, "256") || strings.Contains(diagnostic, "secret-token") {
		t.Fatal(diagnostic)
	}
	diagnostic = githubOperationError(nil, []byte("Authorization: secret-token (HTTP 403)")).Error()
	if !strings.Contains(diagnostic, "HTTP 403") || strings.Contains(diagnostic, "secret-token") {
		t.Fatal(diagnostic)
	}
}
