package system

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// Explicit operator-only probe. Project commands run exclusively inside the
// same offline permission profile and Windows job used by the executor.
func TestLiveNativePHPUnitSandbox(t *testing.T) {
	id := os.Getenv("ORCHESTRATOR_PHP_PROBE_TASK")
	if id == "" {
		t.Skip("operator-only live sandbox probe")
	}
	b, err := os.ReadFile(filepath.Join("..", "..", "config.native.json"))
	if err != nil {
		t.Fatal(err)
	}
	var c Config
	if err = json.Unmarshal(b, &c); err != nil {
		t.Fatal(err)
	}
	r := &Runner{C: c}
	p, err := r.nativePaths(id)
	if err != nil {
		t.Fatal(err)
	}
	home := filepath.Join(filepath.Dir(p.Publish), "executor-runtime")
	if err = prepareNativeTestTools(c, Job{ID: id}, p.Workspace); err != nil {
		t.Fatal(err)
	}
	if err = prepareNativeTaskRuntime(c, home, p.Workspace, ""); err != nil {
		t.Fatal(err)
	}
	php := filepath.Join(nativeTaskTools(home), "php", "php.exe")
	codecept := filepath.Join(nativeTaskTools(home), "codecept.php")
	config := filepath.Join(p.Workspace, "codeception.yml")
	output := filepath.Join(home, "scratch", "unit-suite-probe.log")
	script := "$ErrorActionPreference='Stop'; & " + psQuote(php) + " -v; if ($LASTEXITCODE -ne 0) {throw 'PHP unavailable'}; & " + psQuote(php) + " " + psQuote(codecept) + " " + psQuote(p.Workspace) + " build -c " + psQuote(config) + "; if ($LASTEXITCODE -ne 0) {throw 'Unit actor build failed'}; $ErrorActionPreference='Continue'; & " + psQuote(php) + " " + psQuote(codecept) + " " + psQuote(p.Workspace) + " run unit -o 'coverage: {enabled: false}' --no-colors -c " + psQuote(config) + " 2>&1 | Tee-Object -FilePath " + psQuote(output) + "; 'UNIT_EXIT='+$LASTEXITCODE"
	args := append([]string{"sandbox", "--permission-profile", "orchestrator"}, nativePolicyArgs(c, p.Workspace, home, "executor")...)
	args = append(args, "-C", p.Workspace, "powershell.exe", "-NoProfile", "-NonInteractive", "-Command", script)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, c.CodexPath, args...)
	cmd.Dir = p.Workspace
	cmd.Env = r.nativeEnv(home)
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x4 | 0x08000000}
	job, err := newWindowsJob()
	if err != nil {
		t.Fatal(err)
	}
	defer job.Close()
	cmd.Cancel = job.Terminate
	cmd.WaitDelay = 5 * time.Second
	var log boundedOutput
	cmd.Stdout = &log
	cmd.Stderr = &log
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	if err = job.Assign(cmd.Process.Pid); err != nil {
		cmd.Process.Kill()
		t.Fatal(err)
	}
	if err = resumeNativeProcess(cmd.Process.Pid); err != nil {
		job.Terminate()
		t.Fatal(err)
	}
	err = cmd.Wait()
	text := log.String()
	fmt.Println(text)
	if err != nil {
		t.Fatal("sandbox test launch failed", err)
	}
	if !strings.Contains(text, "Tests:") && !strings.Contains(text, "OK (") {
		t.Fatal("unit suite did not reach test results")
	}
}
