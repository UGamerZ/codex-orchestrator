package system

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"codex-orchestrator/internal/broker"
)

func TestLocalProjectPublicationAndReview(t *testing.T) {
	if _, e := exec.LookPath("git"); e != nil {
		t.Skip("Git required")
	}
	ctx := context.Background()
	root := t.TempDir()
	source := filepath.Join(root, "source")
	run := func(args ...string) {
		t.Helper()
		b, e := exec.Command("git", args...).CombinedOutput()
		if e != nil {
			t.Fatalf("git: %v %s", e, b)
		}
	}
	run("init", "--initial-branch=main", source)
	os.WriteFile(filepath.Join(source, "README.md"), []byte("baseline\n"), 0600)
	run("-C", source, "add", "README.md")
	run("-C", source, "-c", "user.name=Test", "-c", "user.email=test@localhost", "commit", "-m", "baseline")
	c := Config{ProjectMode: "local", LocalRepository: filepath.Join(root, "store.git"), BaseBranch: "main", StateDir: filepath.Join(root, "state"), NativeRoot: filepath.Join(root, "native")}
	run("clone", "--bare", source, c.LocalRepository)
	a := NewAPI(c)
	a.ghTransport = func(context.Context, ...string) ([]byte, error) {
		t.Fatal("local mode attempted GitHub access")
		return nil, nil
	}
	if e := a.ValidateProject(ctx); e != nil {
		t.Fatal(e)
	}
	r := Runner{C: c, API: a}
	j := Job{ID: "task1", Branch: "codex/task1", Title: "Local task"}
	p := broker.Paths{Workspace: filepath.Join(root, "workspace"), Publish: filepath.Join(root, "publisher"), Review: filepath.Join(root, "review")}
	req := broker.Request{Operation: "prepare", Base: c.BaseBranch, Branch: j.Branch}
	if e := r.localRequest(ctx, &req); e != nil {
		t.Fatal(e)
	}
	result, e := broker.Execute(ctx, req, p)
	if e != nil {
		t.Fatal(e)
	}
	j.BaseSHA = result.SHA
	for round := 0; round < 2; round++ {
		if e = os.WriteFile(filepath.Join(p.Workspace, "README.md"), []byte(strings.Repeat("change\n", round+1)), 0600); e != nil {
			t.Fatal(e)
		}
		req = broker.Request{Operation: "publish", Base: c.BaseBranch, Branch: j.Branch, SHA: j.BaseSHA, ExpectedHead: j.Head, Message: "Local change"}
		if e = r.localRequest(ctx, &req); e != nil {
			t.Fatal(e)
		}
		result, e = broker.Execute(ctx, req, p)
		if e != nil {
			t.Fatal(e)
		}
		if e = r.acceptLocal(ctx, &j, "publish", &result); e != nil {
			t.Fatal(e)
		}
		j.Head = result.SHA
		j.PR, j.PRURL, e = a.CreatePR(ctx, &j)
		if e != nil {
			t.Fatal(e)
		}
		pulls, e := a.Pulls(ctx)
		if e != nil || len(pulls) != 1 || pulls[0].Head.SHA != j.Head {
			t.Fatalf("local reviews: %v %+v", e, pulls)
		}
		req = broker.Request{Operation: "review", Base: c.BaseBranch, Branch: j.Branch, SHA: j.Head, PR: j.PR}
		if e = r.localRequest(ctx, &req); e != nil {
			t.Fatal(e)
		}
		if _, e = broker.Execute(ctx, req, p); e != nil {
			t.Fatal(e)
		}
		diff, e := os.ReadFile(filepath.Join(p.Review, "diff.patch"))
		if e != nil || !strings.Contains(string(diff), "change") {
			t.Fatal("review diff missing")
		}
		if e = a.Comment(ctx, j.PR, "Review without approval"); e != nil {
			t.Fatal(e)
		}
	}
	base, e := localGit(ctx, c, "rev-parse", "refs/heads/main")
	if e != nil || strings.TrimSpace(string(base)) != j.BaseSHA {
		t.Fatal("base branch was modified")
	}
	// A human update is never overwritten by automatic publication.
	run("--git-dir="+c.LocalRepository, "update-ref", "refs/heads/"+j.Branch, j.BaseSHA)
	req = broker.Request{Operation: "publish", Base: c.BaseBranch, Branch: j.Branch, SHA: j.BaseSHA, Message: "third"}
	os.WriteFile(filepath.Join(p.Workspace, "README.md"), []byte("third"), 0600)
	if e = r.localRequest(ctx, &req); e != nil {
		t.Fatal(e)
	}
	if _, e = broker.Execute(ctx, req, p); e == nil {
		t.Fatal("external branch change accepted")
	}
}

func TestProjectStateCannotBeReused(t *testing.T) {
	c := Config{Repository: "owner/one", BaseBranch: "main", GithubUser: "alice", StateDir: t.TempDir()}
	if e := bindProject(c); e != nil {
		t.Fatal(e)
	}
	c.Repository = "owner/two"
	if e := bindProject(c); e == nil {
		t.Fatal("project state reused")
	}
}

func TestGitHubProjectRequiresSelectedIdentityAndWriteAccess(t *testing.T) {
	for _, tc := range []struct {
		login  string
		push   bool
		wantOK bool
	}{{"selected", true, true}, {"other", true, false}, {"selected", false, false}} {
		a := NewAPI(Config{GithubUser: "selected", Repository: "owner/project", BaseBranch: "develop"})
		a.ghTransport = func(_ context.Context, args ...string) ([]byte, error) {
			switch args[len(args)-1] {
			case "user":
				return []byte(`{"login":"` + tc.login + `"}`), nil
			case "repos/owner/project":
				if tc.push {
					return []byte(`{"permissions":{"push":true}}`), nil
				}
				return []byte(`{"permissions":{"push":false}}`), nil
			case "repos/owner/project/branches/develop":
				return []byte(`{"name":"develop"}`), nil
			default:
				t.Fatalf("unexpected project request: %v", args)
				return nil, nil
			}
		}
		if e := a.ValidateProject(context.Background()); (e == nil) != tc.wantOK {
			t.Fatalf("identity/access validation: %+v %v", tc, e)
		}
	}
}
