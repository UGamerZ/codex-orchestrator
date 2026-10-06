//go:build !windows

package system

import (
	"context"
	"fmt"
)

func (r *Runner) nativeRun(j Job, role, prompt string) RunResult {
	return RunResult{ID: j.ID, Role: role, Err: fmt.Errorf("native-windows backend requires Windows")}
}
func (r *Runner) nativePreflight(ctx context.Context) error {
	return fmt.Errorf("native-windows backend requires Windows")
}
