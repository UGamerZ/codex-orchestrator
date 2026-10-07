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
	os.MkdirAll(filepath.Join(source, "shared"), 0755)
	os.WriteFile(filepath.Join(source, "shared", "content"), []byte("shared"), 0600)
	if e := os.Symlink("shared", filepath.Join(source, "alias")); e != nil {
		t.Fatal(e)
	}
	run("-C", source, "-c", "core.symlinks=true", "add", "--all")
	run("-C", source, "-c", "user.name=Test", "-c", "user.email=test@localhost", "commit", "-m", "baseline")
	run("-C", source, "branch", "other-work")
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
	contextRequest := broker.Request{Operation: "context", Base: c.BaseBranch, Branch: j.Branch, SHA: j.BaseSHA, Assets: []broker.Asset{{Name: "01-reference.png", Source: "https://example.org/reference.png", Data: []byte("test-reference")}}}
	if e = r.localRequest(ctx, &contextRequest); e != nil {
		t.Fatal(e)
	}
	if _, e = broker.Execute(ctx, contextRequest, p); e != nil {
		t.Fatal(e)
	}
	contextRoot := filepath.Join(p.Review, "context")
	asset, e := os.ReadFile(filepath.Join(contextRoot, "attachments", "01-reference.png"))
	if e != nil || string(asset) != "test-reference" {
		t.Fatalf("context asset: %v", e)
	}
	refs := filepath.Join(contextRoot, "branches.git")
	branch, e := exec.Command("git", "--git-dir="+refs, "rev-parse", "other-work").Output()
	if e != nil || strings.TrimSpace(string(branch)) != j.BaseSHA {
		t.Fatalf("read-only branch missing: %v", e)
	}
	config, e := os.ReadFile(filepath.Join(refs, "config"))
	if e != nil || strings.Contains(string(config), "remote") || strings.Contains(string(config), c.LocalRepository) {
		t.Fatal("reference Git config contains a remote or host path")
	}
	for round := 0; round < 2; round++ {
		if e = os.WriteFile(filepath.Join(p.Workspace, "README.md"), []byte(strings.Repeat("change\n", round+1)), 0600); e != nil {
			t.Fatal(e)
		}
		j.CommitTitle = []string{"Add initial local content", "Extend local content after review"}[round]
		req = broker.Request{Operation: "publish", Base: c.BaseBranch, Branch: j.Branch, SHA: j.BaseSHA, ExpectedHead: j.Head, Message: publicationTitle(&j)}
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
		subject, err := localGit(ctx, c, "log", "-1", "--format=%s", j.Head)
		if err != nil || strings.TrimSpace(string(subject)) != j.CommitTitle {
			t.Fatalf("published commit subject: %s, error: %v", subject, err)
		}
		link, e := localGit(ctx, c, "ls-tree", j.Head, "--", "alias")
		if e != nil || !strings.HasPrefix(string(link), "120000 ") {
			t.Fatal("publication changed the tracked symlink mode")
		}
		linkTarget, e := localGit(ctx, c, "show", j.Head+":alias")
		if e != nil || string(linkTarget) != "shared" {
			t.Fatal("publication changed the tracked symlink target")
		}
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
		reviewResult, reviewErr := broker.Execute(ctx, req, p)
		if reviewErr != nil {
			t.Fatal(reviewErr)
		}
		if !documentationOnly(reviewResult.Files) {
			t.Fatalf("Markdown diff did not activate documentation review: %v", reviewResult.Files)
		}
		diff, e := os.ReadFile(filepath.Join(p.Review, "diff.patch"))
		if e != nil || !strings.Contains(string(diff), "change") {
			t.Fatal("review diff missing")
		}
		if e = a.Comment(ctx, j.PR, "Review without approval"); e != nil {
			t.Fatal(e)
		}
	}
	// An exhausted legacy documentation review is reassessed once, without approval
	// or another automatic fix budget.
	r.C.Backend = "native-windows"
	j.Stage = "waiting"
	j.Role = "executor"
	j.Fixes = 4
	j.Question = "The automatic fix limit has been reached. Findings: wording"
	controller := &Controller{Runner: &r, State: &State{Jobs: map[string]*Job{j.ID: &j}}}
	controller.recoverDocumentationReviews(ctx)
	if j.Stage != "review" || j.Role != "reviewer" || j.Fixes != 4 || j.ReviewPolicyVersion != documentationReviewPolicyVersion {
		t.Fatalf("documentation recovery failed: %+v", j)
	}
	j.Stage = "waiting"
	j.Role = "executor"
	j.Question = "The automatic fix limit has been reached. Findings: still incorrect"
	controller.recoverDocumentationReviews(ctx)
	if j.Stage != "waiting" {
		t.Fatal("documentation recovery looped")
	}
	base, e := localGit(ctx, c, "rev-parse", "refs/heads/main")
	if e != nil || strings.TrimSpace(string(base)) != j.BaseSHA {
		t.Fatal("base branch was modified")
	}
	// A human update is never overwritten by automatic publication.
	os.MkdirAll(filepath.Join(p.Workspace, ".github", "workflows"), 0755)
	os.WriteFile(filepath.Join(p.Workspace, ".github", "workflows", "test.yml"), []byte("name: test\n"), 0644)
	workflowReq := broker.Request{Operation: "publish", Base: c.BaseBranch, Branch: j.Branch, SHA: j.BaseSHA, ExpectedHead: j.Head, Message: "Add test workflow"}
	if e = r.localRequest(ctx, &workflowReq); e != nil {
		t.Fatal(e)
	}
	if _, e = broker.Execute(ctx, workflowReq, p); e == nil || !strings.Contains(e.Error(), "workflow edit needs explicit human permission") {
		t.Fatalf("workflow without approval: %v", e)
	}
	workflowReq.AllowWorkflowEdits = true
	workflowResult, e := broker.Execute(ctx, workflowReq, p)
	if e != nil {
		t.Fatal(e)
	}
	if e = r.acceptLocal(ctx, &j, "publish", &workflowResult); e != nil {
		t.Fatal(e)
	}
	j.Head = workflowResult.SHA
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
