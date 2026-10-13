package system

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestNativeProcessJobKillsSuspendedProcess(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-Command", "Start-Sleep -Seconds 30")
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x4 | 0x08000000}
	job, e := newWindowsJob()
	if e != nil {
		t.Fatal(e)
	}
	defer job.Close()
	if e = cmd.Start(); e != nil {
		t.Fatal(e)
	}
	defer cmd.Process.Kill()
	if e = job.Assign(cmd.Process.Pid); e != nil {
		t.Fatal(e)
	}
	if e = resumeNativeProcess(cmd.Process.Pid); e != nil {
		t.Fatal(e)
	}
	job.Close()
	started := time.Now()
	// Windows can report exit status zero when kill-on-close terminates a job.
	// Completion before the 30-second sleep, without context cancellation,
	// is the observable termination guarantee.
	_ = cmd.Wait()
	if ctx.Err() != nil {
		t.Fatal("process needed context cancellation after job close")
	}
	if time.Since(started) > 5*time.Second {
		t.Fatal("process job termination too slow")
	}
}

func TestNativeJobDeadlineHelper(t *testing.T) {
	role := os.Getenv("ORCHESTRATOR_JOB_DEADLINE_FIXTURE")
	if role == "" {
		return
	}
	if role == "parent" {
		executable, _ := os.Executable()
		child := exec.Command(executable, "-test.run=^TestNativeJobDeadlineHelper$")
		child.Env = append(os.Environ(), "ORCHESTRATOR_JOB_DEADLINE_FIXTURE=child")
		child.Stdout = os.Stdout
		child.Stderr = os.Stderr
		if err := child.Start(); err != nil {
			t.Fatal(err)
		}
	}
	fmt.Println(role + "-ready")
	time.Sleep(30 * time.Second)
}
func TestNativeDeadlineTerminatesChildrenHoldingOutputPipe(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	executable, _ := os.Executable()
	cmd := exec.CommandContext(ctx, executable, "-test.run=^TestNativeJobDeadlineHelper$")
	cmd.Env = append(os.Environ(), "ORCHESTRATOR_JOB_DEADLINE_FIXTURE=parent")
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x4 | 0x08000000}
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	job, err := newWindowsJob()
	if err != nil {
		t.Fatal(err)
	}
	defer job.Close()
	cmd.Cancel = job.Terminate
	cmd.WaitDelay = time.Second
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
	began := time.Now()
	cmd.Wait()
	if !strings.Contains(output.String(), "child-ready") {
		t.Fatal("inherited child fixture did not start")
	}
	if ctx.Err() == nil || time.Since(began) > 8*time.Second {
		t.Fatal("deadline left child/output reader hanging")
	}
}
