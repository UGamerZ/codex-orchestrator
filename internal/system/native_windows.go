package system

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

func (r *Runner) nativeRuntime() string { return filepath.Join(r.C.NativeRoot, "codex-runtime") }
func (r *Runner) nativeEnv(home string) []string {
	return append(withoutEnv(nativeEnvironment(home), "CODEX_HOME"), "CODEX_HOME="+nativeCodexHome(home))
}
func (r *Runner) nativePreflight(ctx context.Context) error {
	if e := NativeNetworkPolicy(false); e != nil {
		return e
	}
	if !r.C.NativeVerified {
		return fmt.Errorf("native isolation has not been verified")
	}
	if e := validateNativeIsolationReceipt(r.C); e != nil {
		return e
	}
	if e := prepareNativeAuth(r.C.AuthFile, r.nativeRuntime()); e != nil {
		return e
	}
	return nil
}

var nativeExecutionMu sync.Mutex

func (r *Runner) nativeRun(j Job, role, prompt string) RunResult {
	nativeExecutionMu.Lock()
	defer nativeExecutionMu.Unlock()
	result := RunResult{ID: j.ID, Role: role}
	if e := NativeNetworkPolicy(false); e != nil {
		result.Err = e
		return result
	}
	ctx, cancel := context.WithTimeout(context.Background(), agentRunTimeout(r.C, role))
	defer cancel()
	p, e := r.nativePaths(j.ID)
	if e != nil {
		result.Err = e
		return result
	}
	for _, dir := range []string{p.Workspace, p.Publish, p.Review} {
		if e = os.MkdirAll(dir, 0700); e != nil {
			result.Err = e
			return result
		}
	}
	if role != "manager" && j.BaseSHA == "" {
		b, e := r.nativeBroker(ctx, &j, "prepare")
		if e != nil {
			result.Err = e
			return result
		}
		result.BaseSHA = b.SHA
	}
	if role == "reviewer" {
		review, e := r.nativeBroker(ctx, &j, "review")
		if e != nil {
			result.Err = e
			return result
		}
		prompt += documentationReviewInstructions(review.Files)
	}
	if role != "manager" {
		if _, e := r.nativeBroker(ctx, &j, "context"); e != nil {
			result.Err = e
			return result
		}
	}
	home := filepath.Join(filepath.Dir(p.Publish), role+"-runtime")
	if e = os.MkdirAll(filepath.Join(home, "scratch"), 0700); e != nil {
		result.Err = e
		return result
	}
	// The parent Codex process is trusted; its credentials stay outside tool-readable roots.
	// Task/role runtimes share only provisioning, never state or conversation history.
	if e = prepareNativeTaskRuntime(r.C, home, p.Workspace, j.Session); e != nil {
		result.Err = e
		return result
	}
	if e = prepareNativeAuth(r.C.AuthFile, nativeCodexHome(home)); e != nil {
		result.Err = e
		return result
	}
	if e = applyNativeTaskScope(ctx, r.C, p.Workspace, home); e != nil {
		result.Err = e
		return result
	}
	output := filepath.Join(home, "result.json")
	os.Remove(output)
	exe, e := os.Executable()
	if e != nil {
		result.Err = e
		return result
	}
	schema := filepath.Join(filepath.Dir(filepath.Dir(exe)), "docker", "result.schema.json")
	args := nativeExecArgs(r.C, j, role, p.Workspace, home, output, schema)
	cmd := exec.CommandContext(ctx, r.C.CodexPath, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x4 | 0x08000000} // Suspend before assigning the job; no child can escape resource limits.
	cmd.Dir = p.Workspace
	cmd.Env = r.nativeEnv(home)
	prompt = nativePrompt(prompt, p.Workspace, p.Review)
	cmd.Stdin = strings.NewReader(prompt)
	var stdout, stderr boundedOutput
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	job, e := newWindowsJob()
	if e != nil {
		result.Err = e
		return result
	}
	defer job.Close()
	cmd.WaitDelay = 5 * time.Second
	cmd.Cancel = func() error {
		if err := job.Terminate(); err != nil {
			return cmd.Process.Kill()
		}
		return nil
	}
	if e = cmd.Start(); e != nil {
		result.Err = fmt.Errorf("native CLI could not start")
		return result
	}
	if e = job.Assign(cmd.Process.Pid); e != nil {
		cmd.Process.Kill()
		cmd.Wait()
		result.Err = e
		return result
	}
	if e = resumeNativeProcess(cmd.Process.Pid); e != nil {
		cmd.Process.Kill()
		cmd.Wait()
		result.Err = e
		return result
	}
	e = cmd.Wait()
	for _, line := range strings.Split(string(stdout.First), "\n") {
		var event struct {
			Type   string `json:"type"`
			Thread string `json:"thread_id"`
		}
		if json.Unmarshal([]byte(line), &event) == nil && event.Type == "thread.started" {
			result.Session = event.Thread
		}
	}
	if result.Session == "" {
		result.Session = j.Session
	}
	if e != nil {
		d := strings.ToLower(stdout.String() + stderr.String())
		result.RateLimited = strings.Contains(d, "usage limit") || strings.Contains(d, "rate limit") || strings.Contains(d, "quota")
		result.ModelUnavailable = strings.Contains(d, "model_not_found") || strings.Contains(d, "model is not supported")
		result.ModelBusy = modelBusy(d)
		result.Err = nativeProcessError(stdout.String(), stderr.String(), e, agentLogSecrets(filepath.Join(filepath.Dir(filepath.Dir(exe)), "config.json"), r.C))
		if ctx.Err() != nil {
			result.TimedOut = true
			result.Err = fmt.Errorf("native agent timed out; process job terminated")
		}
		return result
	}
	b, e := os.ReadFile(output)
	if e != nil || len(b) > 1<<20 {
		result.Err = fmt.Errorf("native agent returned no bounded result")
		return result
	}
	if json.Unmarshal(b, &result.Verdict) != nil {
		result.Err = fmt.Errorf("native agent returned invalid result")
		return result
	}
	result.Err = validateVerdict(role, result.Verdict)
	result.EmptyClarification = result.Err != nil && result.Err.Error() == "empty clarification"
	return result
}

// VerifyNativeIsolation executes test-owned probes, without a model, project or integrations.
// A weaker restricted-token fallback fails the offline-user identity check.
func VerifyNativeIsolation(ctx context.Context, c Config) error {
	if e := NativeNetworkPolicy(false); e != nil {
		return e
	}
	c.StateDir, _ = filepath.Abs(c.StateDir)
	c.AuthFile, _ = filepath.Abs(c.AuthFile)
	r := &Runner{C: c}
	p, e := r.nativePaths("isolation-probe")
	if e != nil {
		return e
	}
	home := filepath.Join(filepath.Dir(p.Publish), "probe-runtime")
	for _, dir := range []string{p.Workspace, p.Review, filepath.Join(home, "scratch"), r.nativeRuntime()} {
		if e = os.MkdirAll(dir, 0700); e != nil {
			return e
		}
	}
	outside := filepath.Join(c.NativeRoot, "isolation-outside.txt")
	if e = os.WriteFile(outside, []byte("TEST SENTINEL, NO SECRETS"), 0600); e != nil {
		return e
	}
	seedUsers := filepath.Join(r.nativeRuntime(), ".sandbox-secrets", "sandbox_users.json")
	if _, err := os.Stat(seedUsers); os.IsNotExist(err) {
		args := append([]string{"sandbox", "--permission-profile", "orchestrator"}, nativePolicyArgs(c, p.Workspace, home, "executor")...)
		args = append(args, "-C", p.Workspace, "powershell.exe", "-NoProfile", "-NonInteractive", "-Command", "whoami.exe")
		cmd := exec.CommandContext(ctx, c.CodexPath, args...)
		cmd.Dir = p.Workspace
		cmd.Env = append(withoutEnv(nativeEnvironment(home), "CODEX_HOME"), "CODEX_HOME="+r.nativeRuntime())
		if b, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("native sandbox provisioning failed: %s", clip(string(b), 1000))
		}
	}
	run := func(role, script string) (string, error) {
		roleHome := filepath.Join(filepath.Dir(p.Publish), role+"-runtime")
		if err := prepareNativeTaskRuntime(c, roleHome, p.Workspace, ""); err != nil {
			return "", err
		}
		if err := applyNativeTaskScope(ctx, c, p.Workspace, roleHome); err != nil {
			return "", err
		}
		args := append([]string{"sandbox", "--permission-profile", "orchestrator"}, nativePolicyArgs(c, p.Workspace, roleHome, role)...)
		args = append(args, "-C", p.Workspace, "powershell.exe", "-NoProfile", "-NonInteractive", "-Command", script)
		cmd := exec.CommandContext(ctx, c.CodexPath, args...)
		cmd.Dir = p.Workspace
		cmd.Env = r.nativeEnv(roleHome)
		b, e := cmd.CombinedOutput()
		if e != nil {
			return string(b), fmt.Errorf("native %s sandbox probe failed; no fallback allowed: %s", role, clip(string(b), 1600))
		}
		return string(b), nil
	}
	marker := filepath.Join(p.Workspace, "probe.txt")
	reviewMarker := filepath.Join(p.Review, "diff.patch")
	if e = os.WriteFile(reviewMarker, []byte("trusted-review-probe"), 0644); e != nil {
		return e
	}
	peer, err := r.nativePaths("isolation-peer")
	if err != nil {
		return err
	}
	if err = os.MkdirAll(peer.Review, 0700); err != nil {
		return err
	}
	peerFile := filepath.Join(peer.Workspace, "peer.txt")
	peerReference := filepath.Join(peer.Review, "peer-reference.txt")
	if err = os.WriteFile(peerReference, []byte("peer reference"), 0600); err != nil {
		return err
	}
	if err = os.WriteFile(peerFile, []byte("other task"), 0600); err != nil {
		return err
	}
	common := "$ErrorActionPreference='Stop'; $actor=(& whoami.exe); if ($actor -notmatch 'CodexSandboxOffline') { throw 'Dedicated offline sandbox user required' };"
	// Give the peer a real sandbox run first, so its permissions are actually
	// provisioned before testing that another task cannot use them.
	runPeer := func() error {
		peerHome := filepath.Join(filepath.Dir(peer.Publish), "executor-runtime")
		if err := prepareNativeTaskRuntime(c, peerHome, peer.Workspace, ""); err != nil {
			return err
		}
		if err := applyNativeTaskScope(ctx, c, peer.Workspace, peerHome); err != nil {
			return err
		}
		args := append([]string{"sandbox", "--permission-profile", "orchestrator"}, nativePolicyArgs(c, peer.Workspace, peerHome, "executor")...)
		script := common + "Set-Content -LiteralPath " + psQuote(peerFile) + " -Value 'peer-working' -NoNewline; if ((Get-Content -LiteralPath " + psQuote(peerFile) + " -Raw) -ne 'peer-working') { throw 'Peer workspace failed' }; if ((Get-Content -LiteralPath " + psQuote(peerReference) + " -Raw) -ne 'peer reference') { throw 'Peer reference failed' }"
		args = append(args, "-C", peer.Workspace, "powershell.exe", "-NoProfile", "-NonInteractive", "-Command", script)
		cmd := exec.CommandContext(ctx, c.CodexPath, args...)
		cmd.Dir = peer.Workspace
		cmd.Env = r.nativeEnv(peerHome)
		if b, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("peer task sandbox failed: %s", clip(string(b), 1200))
		}
		return nil
	}
	if err := runPeer(); err != nil {
		return err
	}
	if _, e = run("executor", common+"Set-Content -LiteralPath "+psQuote(marker)+" -Value 'probe' -NoNewline; if ((Get-Content -LiteralPath "+psQuote(marker)+" -Raw) -ne 'probe') { throw 'Workspace access failed' }; if ((Get-Content -LiteralPath "+psQuote(reviewMarker)+" -Raw) -ne 'trusted-review-probe') { throw 'Executor reference read failed' }; $blocked=$false; try { Set-Content -LiteralPath "+psQuote(reviewMarker)+" -Value 'changed' -NoNewline } catch { $blocked=$true }; if (!$blocked) { throw 'Executor can write trusted references' }; $blocked=$false; try { (Get-Content -LiteralPath "+psQuote(outside)+" -Raw) | Out-Null } catch { $blocked=$true }; if (!$blocked) { throw 'Outside read allowed' }; 'executor-ok'"); e != nil {
		return e
	}
	if _, e = run("reviewer", common+"if ((Get-Content -LiteralPath "+psQuote(marker)+" -Raw) -ne 'probe') { throw 'Review read failed' }; if ((Get-Content -LiteralPath "+psQuote(reviewMarker)+" -Raw) -ne 'trusted-review-probe') { throw 'Review absolute-path read failed' }; if ((Get-Content -LiteralPath "+psQuote(reviewMarker)+" -Raw) -ne 'trusted-review-probe') { throw 'Review material read failed' }; foreach ($target in @("+psQuote(marker)+","+psQuote(reviewMarker)+")) { $blocked=$false; try { Set-Content -LiteralPath $target -Value 'changed' -NoNewline } catch { $blocked=$true }; if (!$blocked) { throw 'Reviewer can write' } }; 'reviewer-ok'"); e != nil {
		return e
	}
	for _, role := range []string{"executor", "reviewer"} {
		script := common + "$blocked=$false; try { (Get-Content -LiteralPath " + psQuote(peerFile) + " -Raw) | Out-Null } catch { $blocked=$true }; if (!$blocked) { throw 'Cross-task read allowed' }; $blocked=$false; try { Set-Content -LiteralPath " + psQuote(peerFile) + " -Value 'changed' -NoNewline } catch { $blocked=$true }; if (!$blocked) { throw 'Cross-task write allowed' }; 'task-isolation-ok'"
		if _, err = run(role, script); err != nil {
			return err
		}
		referenceScript := strings.ReplaceAll(script, psQuote(peerFile), psQuote(peerReference))
		if _, err = run(role, referenceScript); err != nil {
			return err
		}
	}
	if err := runPeer(); err != nil {
		return err
	}
	// Use a native client: .NET constructors are blocked by PowerShell CLM.
	curlProbe := func(address string) string {
		return common + "Get-Command curl.exe -ErrorAction Stop | Out-Null; & curl.exe --silent --insecure --head --output (Join-Path $env:TEMP 'network-probe.txt') --connect-timeout 2 --max-time 4 '" + address + "'; $exit=$LASTEXITCODE; if ($exit -eq 0) { throw 'Offline user reached network endpoint' }; if ($exit -ne 7 -and $exit -ne 28) { throw ('Unexpected network probe exit: '+$exit) }; 'network-blocked'"
	}
	if _, e = run("executor", curlProbe("https://1.1.1.1:443")); e != nil {
		return e
	}
	listener, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		return e
	}
	defer listener.Close()
	go http.Serve(listener, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	address := "http://" + listener.Addr().String()
	client := &http.Client{Timeout: 3 * time.Second}
	response, e := client.Get(address)
	if e != nil {
		return fmt.Errorf("host loopback control probe failed: %w", e)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		return fmt.Errorf("invalid loopback control response")
	}
	if _, e = run("executor", curlProbe(address)); e != nil {
		return e
	}
	cli, e := os.ReadFile(c.CodexPath)
	if e != nil {
		return e
	}
	sum := sha256.Sum256(cli)
	receipt, _ := json.Marshal(map[string]any{"version": 3, "codex_sha256": hex.EncodeToString(sum[:]), "verified_at": time.Now().UTC()})
	return os.WriteFile(filepath.Join(c.NativeRoot, "isolation.json"), receipt, 0600)
}
