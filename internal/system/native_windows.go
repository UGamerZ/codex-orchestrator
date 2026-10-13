package system

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

func (r *Runner) nativeRuntime() string { return filepath.Join(r.C.NativeRoot, "codex-runtime") }
func (r *Runner) nativeEnv(home string) []string {
	return append(withoutEnv(nativeEnvironment(home), "CODEX_HOME"), "CODEX_HOME="+r.nativeRuntime())
}
func (r *Runner) nativePreflight(ctx context.Context) error {
	if e := NativeNetworkPolicy(false); e != nil {
		return e
	}
	if !r.C.NativeVerified {
		return fmt.Errorf("native isolation has not been verified")
	}
	b, e := os.ReadFile(filepath.Join(r.C.NativeRoot, "isolation.json"))
	if e != nil {
		return fmt.Errorf("native isolation receipt missing; run Check-NativeIsolation.ps1")
	}
	var receipt struct {
		Version int    `json:"version"`
		Codex   string `json:"codex_sha256"`
	}
	if json.Unmarshal(b, &receipt) != nil || receipt.Version != 2 {
		return fmt.Errorf("invalid isolation receipt")
	}
	cli, e := os.ReadFile(r.C.CodexPath)
	if e != nil {
		return e
	}
	sum := sha256.Sum256(cli)
	if receipt.Codex != hex.EncodeToString(sum[:]) {
		return fmt.Errorf("Codex binary changed; rerun native isolation check")
	}
	if e := prepareNativeAuth(r.C.AuthFile, r.nativeRuntime()); e != nil {
		return e
	}
	return nil
}
func (r *Runner) nativeRun(j Job, role, prompt string) RunResult {
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
	// A shared native runtime avoids racing setup/password changes for sandbox users.
	if _, e = os.Stat(filepath.Join(r.nativeRuntime(), "auth.json")); e != nil {
		result.Err = fmt.Errorf("native Codex login unavailable")
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
		result.Err = fmt.Errorf("native agent failed inside sandbox; no unrestricted retry is permitted")
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
	run := func(role, script string) (string, error) {
		args := append([]string{"sandbox", "--permission-profile", "orchestrator"}, nativePolicyArgs(c, p.Workspace, home, role)...)
		args = append(args, "-C", p.Workspace, "powershell.exe", "-NoProfile", "-NonInteractive", "-Command", script)
		cmd := exec.CommandContext(ctx, c.CodexPath, args...)
		cmd.Dir = p.Workspace
		cmd.Env = r.nativeEnv(home)
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
	common := "$ErrorActionPreference='Stop'; $actor=[Security.Principal.WindowsIdentity]::GetCurrent().Name; if ($actor -notmatch 'CodexSandboxOffline') { throw 'Dedicated offline sandbox user required' };"
	if _, e = run("executor", common+"[IO.File]::WriteAllText("+psQuote(marker)+",'probe'); if ([IO.File]::ReadAllText("+psQuote(marker)+") -ne 'probe') { throw 'Workspace access failed' }; if ([IO.File]::ReadAllText("+psQuote(reviewMarker)+") -ne 'trusted-review-probe') { throw 'Executor reference read failed' }; $blocked=$false; try { [IO.File]::WriteAllText("+psQuote(reviewMarker)+",'changed') } catch { $blocked=$true }; if (!$blocked) { throw 'Executor can write trusted references' }; $blocked=$false; try { [IO.File]::ReadAllText("+psQuote(outside)+") | Out-Null } catch { $blocked=$true }; if (!$blocked) { throw 'Outside read allowed' }; 'executor-ok'"); e != nil {
		return e
	}
	if _, e = run("reviewer", common+"if ([IO.File]::ReadAllText("+psQuote(marker)+") -ne 'probe') { throw 'Review read failed' }; if ((Get-Content -LiteralPath "+psQuote(reviewMarker)+" -Raw) -ne 'trusted-review-probe') { throw 'Review absolute-path read failed' }; if ([IO.File]::ReadAllText("+psQuote(reviewMarker)+") -ne 'trusted-review-probe') { throw 'Review material read failed' }; foreach ($target in @("+psQuote(marker)+","+psQuote(reviewMarker)+")) { $blocked=$false; try { [IO.File]::WriteAllText($target,'changed') } catch { $blocked=$true }; if (!$blocked) { throw 'Reviewer can write' } }; 'reviewer-ok'"); e != nil {
		return e
	}
	if _, e = run("executor", common+"$client=New-Object Net.Sockets.TcpClient; $attempt=$client.BeginConnect('1.1.1.1',443,$null,$null); if ($attempt.AsyncWaitHandle.WaitOne(4000)) { try { $client.EndConnect($attempt) } catch {} }; if ($client.Connected) { throw 'Offline user reached internet' }; $client.Close(); 'network-ok'"); e != nil {
		return e
	}
	listener, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		return e
	}
	defer listener.Close()
	port := listener.Addr().(*net.TCPAddr).Port
	if _, e = run("executor", common+fmt.Sprintf("$client=New-Object Net.Sockets.TcpClient; try { $client.Connect('127.0.0.1',%d) } catch {}; if ($client.Connected) { throw 'Offline user reached host loopback' }; $client.Close(); 'loopback-ok'", port)); e != nil {
		return e
	}
	cli, e := os.ReadFile(c.CodexPath)
	if e != nil {
		return e
	}
	sum := sha256.Sum256(cli)
	receipt, _ := json.Marshal(map[string]any{"version": 2, "codex_sha256": hex.EncodeToString(sum[:]), "verified_at": time.Now().UTC()})
	return os.WriteFile(filepath.Join(c.NativeRoot, "isolation.json"), receipt, 0600)
}
