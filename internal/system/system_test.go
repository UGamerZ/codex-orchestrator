package system

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"codex-orchestrator/internal/broker"
)

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func response(body string) *http.Response {
	return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{}}
}
func fixture(t *testing.T) *Controller {
	t.Helper()
	c := Config{Repository: "owner/repo", BaseBranch: "dev", StateDir: t.TempDir(), MaxWorkers: 2, MaxFixes: 3, Chat: 123, ManagerInterval: 3600}
	a := NewAPI(c)
	return &Controller{C: c, API: a, State: &State{Jobs: map[string]*Job{}}, Results: make(chan RunResult, 8), ChannelReady: true}
}
func fakePull(a *API, head string) {
	a.ghTransport = func(_ context.Context, args ...string) ([]byte, error) {
		joined := strings.Join(args, " ")
		if strings.Contains(joined, "/pulls?") {
			return []byte(`[[{"number":7,"head":{"sha":"` + head + `"},"base":{"ref":"dev"}}]]`), nil
		}
		if strings.Contains(joined, "/issues/7/comments") {
			return []byte(`{}`), nil
		}
		panic("unexpected github command: " + joined)
	}
}
func TestReviewerCannotApprove(t *testing.T) {
	if validateVerdict("reviewer", Verdict{Status: "approve"}) == nil {
		t.Fatal("approve accepted")
	}
	if validateVerdict("reviewer", Verdict{Status: "completed", Summary: "merged"}) == nil {
		t.Fatal("executor result accepted by reviewer")
	}
	if validateVerdict("reviewer", Verdict{Status: "passed", Findings: []string{"bug"}}) == nil {
		t.Fatal("inconsistent verdict accepted")
	}
	c := fixture(t)
	j := &Job{ID: "task", PR: 7, Head: "abc", Stage: "running", Role: "reviewer"}
	c.State.Jobs[j.ID] = j
	fakePull(c.API, "abc")
	if e := c.finish(context.Background(), RunResult{ID: j.ID, Role: "reviewer", Verdict: Verdict{Status: "passed", Summary: "ok"}}); e != nil {
		t.Fatal(e)
	}
	if j.Stage != "passed" {
		t.Fatalf("stage %s", j.Stage)
	}
	// fakePull panics for any reviews, approve or merge endpoint.
}
func TestAutomaticFixSameBranchAndSession(t *testing.T) {
	c := fixture(t)
	j := &Job{ID: "task", PR: 7, Head: "abc", Branch: "codex/task", Stage: "running", Role: "reviewer", ExecutorSession: "executor-original"}
	c.State.Jobs[j.ID] = j
	fakePull(c.API, "abc")
	e := c.finish(context.Background(), RunResult{ID: j.ID, Role: "reviewer", Session: "review-session", Verdict: Verdict{Status: "changes", Findings: []string{"Fix broken authorization"}}})
	if e != nil {
		t.Fatal(e)
	}
	if j.Stage != "queued" || j.Role != "executor" || j.Branch != "codex/task" || j.ExecutorSession != "executor-original" || j.ReviewerSession != "review-session" {
		t.Fatalf("lost branch/session: %+v", j)
	}
}
func TestStaleReviewNeverPasses(t *testing.T) {
	c := fixture(t)
	j := &Job{ID: "task", PR: 7, Head: "old", Stage: "running", Role: "reviewer"}
	c.State.Jobs[j.ID] = j
	fakePull(c.API, "new")
	if e := c.finish(context.Background(), RunResult{ID: j.ID, Role: "reviewer", Verdict: Verdict{Status: "passed"}}); e != nil {
		t.Fatal(e)
	}
	if j.Stage != "stale" || j.NotifiedHead != "" {
		t.Fatal("stale review was accepted")
	}
}
func TestReplyRoutesOnlyToAskingAgent(t *testing.T) {
	c := fixture(t)
	a := &Job{ID: "a", Role: "executor", Stage: "waiting", QuestionMessage: 100, ExecutorSession: "session-a"}
	b := &Job{ID: "b", Role: "reviewer", Stage: "waiting", QuestionMessage: 101, ReviewerSession: "session-b"}
	c.State.Jobs[a.ID] = a
	c.State.Jobs[b.ID] = b
	c.API.Client.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
		return response(`{"ok":true,"result":[{"update_id":1,"message":{"message_id":102,"text":"Use the uploaded fixture","chat":{"id":123,"type":"private"},"reply_to_message":{"message_id":101}}}]}`), nil
	})
	if e := c.answers(context.Background()); e != nil {
		t.Fatal(e)
	}
	if a.Stage != "waiting" || a.Answer != "" || b.Stage != "review" || b.Answer == "" || b.ReviewerSession != "session-b" {
		t.Fatal("wrong agent resumed")
	}
}
func TestOtherChatCannotResume(t *testing.T) {
	c := fixture(t)
	j := &Job{ID: "a", Role: "executor", Stage: "waiting", QuestionMessage: 100}
	c.State.Jobs[j.ID] = j
	c.API.Client.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
		return response(`{"ok":true,"result":[{"update_id":1,"message":{"text":"do it","chat":{"id":999,"type":"private"},"reply_to_message":{"message_id":100}}}]}`), nil
	})
	if e := c.answers(context.Background()); e != nil {
		t.Fatal(e)
	}
	if j.Stage != "waiting" {
		t.Fatal("unauthorized chat resumed job")
	}
}
func TestConcurrencyLimit(t *testing.T) {
	c := fixture(t)
	for _, id := range []string{"a", "b"} {
		c.State.Jobs[id] = &Job{ID: id, Role: "executor", Stage: "running"}
	}
	c.State.Jobs["c"] = &Job{ID: "c", Role: "executor", Stage: "queued"}
	if e := c.schedule(); e != nil {
		t.Fatal(e)
	}
	if c.State.Jobs["c"].Stage != "queued" {
		t.Fatal("third executor launched")
	}
}
func TestQuotaDoesNotSwitchToFallback(t *testing.T) {
	c := fixture(t)
	j := &Job{ID: "task", Role: "executor", Stage: "running"}
	c.State.Jobs[j.ID] = j
	if e := c.finish(context.Background(), RunResult{ID: j.ID, Role: "executor", Err: io.EOF, RateLimited: true}); e != nil {
		t.Fatal(e)
	}
	if j.Fallback || j.Stage != "queued" || time.Until(j.RetryAt) < 59*time.Minute {
		t.Fatal("quota handling incorrect")
	}
}
func TestManagerCannotInventCard(t *testing.T) {
	c := fixture(t)
	c.State.Pending = []*Job{{ID: "known"}}
	j := &Job{ID: "manager", Role: "manager", Stage: "running"}
	c.State.Jobs[j.ID] = j
	if e := c.finish(context.Background(), RunResult{ID: j.ID, Role: "manager", Verdict: Verdict{Status: "completed", Selected: []string{"invented"}}}); e != nil {
		t.Fatal(e)
	}
	if len(c.State.Jobs) != 1 || j.Stage != "waiting" {
		t.Fatal("untrusted card scheduled")
	}
}
func TestCrashRecoveryPauses(t *testing.T) {
	dir := t.TempDir()
	s := State{Jobs: map[string]*Job{"a": {ID: "a", Stage: "running", Role: "executor"}, "b": {ID: "b", Stage: "publishing", Role: "publisher"}}}
	if e := s.Save(dir); e != nil {
		t.Fatal(e)
	}
	loaded, e := LoadState(dir)
	if e != nil {
		t.Fatal(e)
	}
	for _, j := range loaded.Jobs {
		if j.Stage != "waiting" || j.Question == "" {
			t.Fatal("crashed job silently restarted")
		}
	}
}
func TestPublishedButPRFailedIsRecoverable(t *testing.T) {
	c := fixture(t)
	j := &Job{ID: "a", BaseSHA: "base", Head: "published", Stage: "publishing"}
	c.State.Jobs[j.ID] = j
	c.API.ghTransport = func(_ context.Context, args ...string) ([]byte, error) {
		if strings.Contains(strings.Join(args, " "), "state=all") {
			return []byte(`[{"number":7,"html_url":"https://github.com/owner/repo/pull/7"}]`), nil
		}
		panic("unexpected request")
	}
	if e := c.finish(context.Background(), RunResult{ID: j.ID, Role: "publisher", Publication: broker.Result{SHA: "published", Changed: false}}); e != nil {
		t.Fatal(e)
	}
	if j.PR != 7 || j.Stage != "review" {
		t.Fatalf("recovery failed %+v", j)
	}
}
func TestSecretsNotInherited(t *testing.T) {
	env := withoutEnv([]string{"PATH=x", "GH_TOKEN=secret", "Telegram_Token=secret", "CODEX_API_KEY=secret"}, "GH_TOKEN", "TELEGRAM_TOKEN", "CODEX_API_KEY")
	if len(env) != 1 || env[0] != "PATH=x" {
		t.Fatal("secret environment retained")
	}
}
func TestConfigRefusesUnsafeActivation(t *testing.T) {
	b, e := os.ReadFile("../../config.example.json")
	if e != nil {
		t.Fatal(e)
	}
	var c Config
	if e = json.Unmarshal(b, &c); e != nil {
		t.Fatal(e)
	}
	c.MaxWorkers = 3
	p := t.TempDir() + "/config.json"
	b, _ = json.Marshal(c)
	os.WriteFile(p, b, 0600)
	if _, e = LoadConfig(p); e == nil {
		t.Fatal("unsafe concurrency configuration enabled")
	}
}
func TestOutputBoundPreservesThreadAndVerdict(t *testing.T) {
	var b boundedOutput
	b.Write([]byte(`{"type":"thread.started","thread_id":"original"}` + "\n"))
	for i := 0; i < 100; i++ {
		b.Write([]byte(strings.Repeat("x", 128<<10)))
	}
	b.Write([]byte("ORCHESTRATOR_RESULT={\"status\":\"passed\"}"))
	if len(b.First) > 128<<10 || len(b.Tail) > 8<<20 {
		t.Fatal("unbounded output")
	}
	if !strings.Contains(string(b.First), "original") || !strings.Contains(b.String(), "ORCHESTRATOR_RESULT=") {
		t.Fatal("lost thread/verdict")
	}
}
func TestNoWorkWithoutQuestionChannel(t *testing.T) {
	c := fixture(t)
	c.ChannelReady = false
	c.State.Jobs["a"] = &Job{ID: "a", Stage: "queued", Role: "executor"}
	if e := c.schedule(); e != nil {
		t.Fatal(e)
	}
	if c.State.Jobs["a"].Stage != "queued" {
		t.Fatal("agent started without question channel")
	}
}
