package system

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

func (c *Controller) deleteCompletedTask(id string) error {
	j := c.State.Jobs[id]
	if j == nil || id == "manager" || !safeID.MatchString(id) || (!j.BoardCompleted && !j.ClosedPR) || j.Stage == "running" || j.Stage == "publishing" {
		return fmt.Errorf("only a stopped task with an archived card or closed PR can be removed")
	}
	archive := filepath.Join(c.C.StateDir, "deleted-tasks")
	if err := os.MkdirAll(archive, 0700); err != nil {
		return err
	}
	if err := writePrivateJSON(filepath.Join(archive, id+".json"), j); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if err := c.Runner.removeMergedCopies(ctx, j); err != nil {
		return err
	}
	delete(c.State.Jobs, id)
	pending := c.State.Pending[:0]
	for _, task := range c.State.Pending {
		if task.ID != id {
			pending = append(pending, task)
		}
	}
	c.State.Pending = pending
	c.panelEvent("task-deleted", id, "Removed cancelled task and reproducible working copies; local history, logs and remote commits retained")
	return nil
}
