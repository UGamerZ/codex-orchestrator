package system

import (
	"context"
	"os/exec"
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
