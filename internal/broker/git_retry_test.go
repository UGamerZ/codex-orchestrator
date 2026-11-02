package broker

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestGitOutputRetryProcess(t *testing.T) {
	if os.Getenv("ORCHESTRATOR_GIT_RETRY_CHILD") != "1" {
		return
	}
	if os.Getenv("ORCHESTRATOR_GIT_RETRY_ATTEMPT") == "1" {
		fmt.Fprintln(os.Stderr, os.Getenv("ORCHESTRATOR_GIT_RETRY_FAILURE"))
		os.Exit(1)
	}
	fmt.Print("pushed")
	os.Exit(0)
}

func TestGitOutputCancellationDuringRetry(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	calls := 0
	start := time.Now()
	_, err := gitOutput(ctx, "fetch", "", func() *exec.Cmd {
		calls++
		cmd := exec.Command(os.Args[0], "-test.run=^TestGitOutputRetryProcess$")
		cmd.Env = append(os.Environ(), "ORCHESTRATOR_GIT_RETRY_CHILD=1", "ORCHESTRATOR_GIT_RETRY_ATTEMPT=1", "ORCHESTRATOR_GIT_RETRY_FAILURE=Connection was reset")
		cancel()
		return cmd
	})
	if err == nil || calls != 1 || time.Since(start) > time.Second {
		t.Fatalf("cancel did not prevent retry: %d %v", calls, err)
	}
}

func TestGitOutputTransportRetries(t *testing.T) {
	for _, tc := range []struct {
		operation, failure string
		calls              int
	}{
		{"push", "temporary failure", 2},
		{"fetch", "Recv failure: Connection was reset", 2},
		{"ls-remote", "Could not resolve host: github.com", 2},
		{"fetch", "Authentication failed", 1},
		{"fetch", "SSL certificate problem", 1},
		{"checkout", "Connection was reset", 1},
	} {
		operation := tc.operation
		calls := 0
		b, e := gitOutput(context.Background(), operation, "", func() *exec.Cmd {
			calls++
			cmd := exec.Command(os.Args[0], "-test.run=^TestGitOutputRetryProcess$")
			cmd.Env = append(os.Environ(), "ORCHESTRATOR_GIT_RETRY_CHILD=1", "ORCHESTRATOR_GIT_RETRY_ATTEMPT="+strconv.Itoa(calls), "ORCHESTRATOR_GIT_RETRY_FAILURE="+tc.failure)
			return cmd
		})
		if tc.calls == 2 {
			if calls != 2 || e != nil || string(b) != "pushed" {
				t.Fatalf("push retries: %d %v %s", calls, e, b)
			}
		} else if calls != 1 || e == nil || !strings.Contains(e.Error(), tc.failure) {
			t.Fatalf("fetch diagnostic: %d %v", calls, e)
		}
	}
}
