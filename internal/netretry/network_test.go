package netretry

import "testing"

func TestReadFailureClassification(t *testing.T) {
	for _, tc := range []struct {
		message string
		want    bool
	}{
		{"trusted Git operation failed: fetch: Recv failure: Connection was reset", true},
		{"trusted Git operation failed: ls-remote: Could not resolve host: github.com", true},
		{"trusted Git operation failed: fetch: The requested URL returned error: 503", true},
		{"trusted Git operation failed: push: Connection was reset", false},
		{"trusted Git operation failed: fetch: Authentication failed; connection was reset", false},
		{"trusted Git operation failed: fetch: SSL certificate problem", false},
		{"trusted Git operation failed: fetch: couldn't find remote ref", false},
		{"workflow edit needs explicit human permission", false},
		{"native agent: Connection was reset", false},
	} {
		if got := ReadFailure(tc.message); got != tc.want {
			t.Errorf("%q: got %v want %v", tc.message, got, tc.want)
		}
	}
}
