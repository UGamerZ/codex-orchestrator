package system

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

func TestDiscoveryConfirmsChangedBranch(t *testing.T) {
	for _, tc := range []struct {
		name, actual string
		unavailable  bool
	}{
		{"stale PR list after own publication", "published", false},
		{"real outside edit", "human", false},
		{"cannot confirm branch", "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := fixture(t)
			j := &Job{ID: "task", PR: 7, Head: "published", ReviewedHead: "published", Stage: "review", Branch: "codex/task"}
			c.State.Jobs[j.ID] = j
			c.API.ghTransport = func(_ context.Context, args ...string) ([]byte, error) {
				cmd := strings.Join(args, " ")
				if strings.Contains(cmd, "/pulls?") {
					return []byte(`[[{"number":7,"head":{"sha":"previous","ref":"codex/task","repo":{"full_name":"owner/repo"}}}]]`), nil
				}
				if strings.Contains(cmd, "/git/ref/heads/codex%2Ftask") {
					if tc.unavailable {
						return nil, fmt.Errorf("offline")
					}
					return []byte(`{"object":{"sha":"` + tc.actual + `"}}`), nil
				}
				t.Fatalf("unexpected request %s", cmd)
				return nil, nil
			}
			err := c.discoverPRs(context.Background())
			if tc.unavailable && err == nil {
				t.Fatal("missing branch confirmation accepted")
			}
			if !tc.unavailable && err != nil {
				t.Fatal(err)
			}
			if tc.actual == "human" {
				if !j.External || j.Stage != "waiting" || j.Head != "human" || j.ReviewedHead != "" {
					t.Fatalf("outside edit not blocked: %+v", j)
				}
			} else if j.External || j.Stage != "review" || j.Head != "published" {
				t.Fatalf("own publication blocked: %+v", j)
			}
		})
	}
}
