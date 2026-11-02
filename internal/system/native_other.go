//go:build !windows

package system

import (
	"context"
	"fmt"
)

func nativeCanSetACL(path string) bool { return true }

func (r *Runner) nativeRun(parent context.Context, j Job, role, prompt string) RunResult {
	return RunResult{ID: j.ID, Role: role, Err: fmt.Errorf("native-windows backend requires Windows")}
}
func (r *Runner) nativePreflight(ctx context.Context) error {
	return fmt.Errorf("native-windows backend requires Windows")
}
