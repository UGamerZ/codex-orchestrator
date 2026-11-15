package system

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// External review snapshots of our own tasks remain managed. Synthetic pr-N
// jobs imported from repository-wide polling have no task provenance.
func importedPull(j *Job) bool {
	return j != nil && j.External && strings.HasPrefix(j.ID, "pr-") && !systemCreatedTask(j)
}

func (c *Controller) retireImportedPulls() error {
	for id, j := range c.State.Jobs {
		if !importedPull(j) {
			continue
		}
		if !safeID.MatchString(id) || j.Stage == "running" || j.Stage == "publishing" {
			return fmt.Errorf("cannot retire an active or invalid imported PR job")
		}
		archive := filepath.Join(c.C.StateDir, "ignored-external-prs")
		if err := os.MkdirAll(archive, 0700); err != nil {
			return err
		}
		if err := writePrivateJSON(filepath.Join(archive, id+".json"), j); err != nil {
			return err
		}
		delete(c.State.Jobs, id)
		c.panelEvent("ignored-pr", id, "Removed unrelated GitHub PR from managed tasks; history archived locally. No GitHub or Trello changes were made.")
	}
	return nil
}
