package system

import "context"

// The controller owns this registry. Workers receive only the context; the entry
// stays registered until their result arrives, so switching cannot race cleanup.
func (c *Controller) taskRunContext(id string) context.Context {
	if c.ActiveRuns == nil {
		c.ActiveRuns = make(map[string]context.CancelFunc)
	}
	ctx, cancel := context.WithCancel(context.Background())
	c.ActiveRuns[id] = cancel
	return ctx
}

func (c *Controller) releaseTaskRun(id string) {
	if cancel := c.ActiveRuns[id]; cancel != nil {
		cancel()
		delete(c.ActiveRuns, id)
	}
}
