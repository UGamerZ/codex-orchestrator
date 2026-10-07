package broker

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
)

func TestGitOutputRetryProcess(t *testing.T) {
	if os.Getenv("ORCHESTRATOR_GIT_RETRY_CHILD") != "1" {
		return
	}
	if os.Getenv("ORCHESTRATOR_GIT_RETRY_ATTEMPT") == "1" {
		fmt.Fprintln(os.Stderr, "temporary failure")
		os.Exit(1)
	}
	fmt.Print("pushed")
	os.Exit(0)
}

func TestGitOutputRetriesOnlyPush(t *testing.T) {
	for _, operation := range []string{"push", "fetch"} {
		calls := 0
		b, e := gitOutput(context.Background(), operation, "", func() *exec.Cmd {
			calls++
			cmd := exec.Command(os.Args[0], "-test.run=^TestGitOutputRetryProcess$")
			cmd.Env = append(os.Environ(), "ORCHESTRATOR_GIT_RETRY_CHILD=1", "ORCHESTRATOR_GIT_RETRY_ATTEMPT="+strconv.Itoa(calls))
			return cmd
		})
		if operation == "push" {
			if calls != 2 || e != nil || string(b) != "pushed" {
				t.Fatalf("push retries: %d %v %s", calls, e, b)
			}
		} else if calls != 1 || e == nil || !strings.Contains(e.Error(), "temporary failure") {
			t.Fatalf("fetch diagnostic: %d %v", calls, e)
		}
	}
}
