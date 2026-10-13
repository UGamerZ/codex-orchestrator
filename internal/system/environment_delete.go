package system

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Keep ownership records and files so shared-repository PR discovery cannot
// mistake deleted environments' existing work for new external tasks.
func (c *Controller) deleteEnvironment(id string) error {
	if !environmentID.MatchString(id) {
		return fmt.Errorf("invalid environment")
	}
	r, err := readRegistry(c.ConfigPath)
	if err != nil {
		return err
	}
	target := -1
	for i, p := range r.Profiles {
		if p.ID == id && !p.Deleted {
			target = i
			break
		}
	}
	if target < 0 {
		return fmt.Errorf("environment not found")
	}
	p := r.Profiles[target]
	targetPath, err := filepath.Abs(p.Config.StateDir)
	if err != nil {
		return err
	}
	activePath, err := filepath.Abs(c.C.StateDir)
	if err != nil {
		return err
	}
	if id == r.Active || strings.EqualFold(filepath.Clean(targetPath), filepath.Clean(activePath)) {
		return fmt.Errorf("switch to another environment before deleting the active environment")
	}
	// A live second controller or unfinished run must not become orphaned.
	if _, err = os.Stat(targetPath); err == nil {
		lockPath := filepath.Join(targetPath, "controller.lock")
		lock, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return fmt.Errorf("environment is locked; inspect its controller before deleting")
		}
		fmt.Fprint(lock, os.Getpid())
		lock.Close()
		defer os.Remove(lockPath)
		var state State
		b, err := os.ReadFile(filepath.Join(targetPath, "state.json"))
		if err != nil && !os.IsNotExist(err) {
			return err
		}
		if err == nil {
			if err = json.Unmarshal(b, &state); err != nil {
				return err
			}
			if state.Jobs == nil {
				return fmt.Errorf("invalid saved jobs")
			}
		}
		for _, j := range state.Jobs {
			if j.Stage == "running" || j.Stage == "publishing" {
				return fmt.Errorf("environment has unfinished active work; switch to it and let recovery finish before deleting")
			}
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	r.Profiles[target].Deleted = true
	if err := writePrivateJSON(registryPath(c.ConfigPath), r); err != nil {
		return err
	}
	c.panelEvent("environment", "", "Deleted environment: "+p.Name+". Working files and task history retained.")
	return nil
}
