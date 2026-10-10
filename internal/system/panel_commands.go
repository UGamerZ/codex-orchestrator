package system

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"
)

type PanelEvent struct {
	At      time.Time `json:"at"`
	Kind    string    `json:"kind"`
	Task    string    `json:"task"`
	Message string    `json:"message"`
}
type PanelCommand struct {
	Action string                     `json:"action"`
	ID     string                     `json:"id"`
	Text   string                     `json:"text"`
	Patch  map[string]json.RawMessage `json:"patch"`
	Result chan error                 `json:"-"`
}

func (c *Controller) panelEvent(kind, id, message string) {
	if len(c.State.Events) > 0 {
		last := c.State.Events[len(c.State.Events)-1]
		if last.Kind == kind && last.Task == id && last.Message == message && time.Since(last.At) < time.Minute {
			return
		}
	}
	c.State.Events = append(c.State.Events, PanelEvent{time.Now().UTC(), kind, id, clip(message, 2000)})
	if len(c.State.Events) > 200 {
		c.State.Events = c.State.Events[len(c.State.Events)-200:]
	}
}
func (c *Controller) reportPanelIssue(ctx context.Context, label string, err error) {
	c.panelEvent("system-error", "", label+": "+err.Error())
	if c.State.IssueWarnings == nil {
		c.State.IssueWarnings = map[string]time.Time{}
	}
	if time.Since(c.State.IssueWarnings[label]) < time.Hour {
		return
	}
	if _, e := c.API.Send(ctx, "System issue: "+label+"\n"+err.Error()+"\nTasks and known sessions are retained. Check the dashboard for current status."); e == nil {
		c.State.IssueWarnings[label] = time.Now().UTC()
	}
}
func (c *Controller) recoverDataGuardPublications() {
	for _, j := range c.State.Jobs {
		if j.Stage == "waiting" && j.Role == "publisher" && !j.External && !j.ClosedPR && j.DataGuardVersion < 1 && strings.HasPrefix(j.LastError, "data-preservation guard:") {
			j.DataGuardVersion = 1
			j.Stage = "publish"
			j.Question = ""
			j.QuestionMessage = 0
			c.panelEvent("retry", j.ID, "Retrying publication after SQL comment false-positive fix")
		}
	}
}
func (c *Controller) handlePanelCommand(cmd PanelCommand) {
	err := c.executePanelCommand(cmd)
	if err == nil {
		err = c.save()
	}
	cmd.Result <- err
}
func (c *Controller) executePanelCommand(cmd PanelCommand) error {
	switch cmd.Action {
	case "check":
		c.State.BoardCheckRequested = true
	case "pause":
		c.State.Paused = true
	case "resume":
		c.State.Paused = false
	case "config":
		if _, err := c.validatedPanelConfig(cmd.Patch); err != nil {
			return err
		}
		c.State.PendingConfig = cmd.Patch
	case "retry", "answer":
		j := c.State.Jobs[cmd.ID]
		if j == nil || j.ClosedPR || j.BoardCompleted {
			return fmt.Errorf("task unavailable")
		}
		if j.Stage == "running" || j.Stage == "publishing" {
			return fmt.Errorf("worker already active")
		}
		if j.Stage != "waiting" && j.Stage != "queued" && j.Stage != "review" {
			return fmt.Errorf("only waiting or queued tasks can be retried")
		}
		if cmd.Action == "answer" {
			if strings.TrimSpace(cmd.Text) == "" {
				return fmt.Errorf("answer required")
			}
			j.Clarifications = append(j.Clarifications, Clarification{Role: j.Role, Origin: j.QuestionOrigin, Question: j.Question, Answer: cmd.Text, At: time.Now().UTC()})
			j.Answer = cmd.Text
		} else if j.Stage == "waiting" && j.QuestionOrigin == "agent" {
			return fmt.Errorf("this task needs an answer, not an empty retry")
		}
		j.Question = ""
		j.QuestionMessage = 0
		j.RetryAt = time.Time{}
		j.StallRetries = 0
		j.ModelBusyRetries = 0
		j.InvalidResultRetries = 0
		j.Stage = "queued"
		if j.Role == "reviewer" {
			j.Stage = "review"
		}
		if j.Role == "publisher" {
			j.Stage = "publish"
		}
	default:
		return fmt.Errorf("unknown action")
	}
	c.panelEvent("operator", cmd.ID, cmd.Action+" requested")
	return nil
}

var panelFields = map[string]bool{"worker_model": true, "worker_effort": true, "fallback_model": true, "fallback_effort": true, "manager_model": true, "reviewer_model": true, "other_effort": true, "max_workers": true, "max_fix_rounds": true, "run_timeout_minutes": true, "manager_interval_seconds": true, "auto_continue_task_branches": true}

func (c *Controller) validatedPanelConfig(patch map[string]json.RawMessage) (Config, error) {
	b, _ := json.Marshal(c.C)
	var values map[string]json.RawMessage
	json.Unmarshal(b, &values)
	for k, v := range patch {
		if !panelFields[k] {
			return Config{}, fmt.Errorf("configuration field %s is not editable here", k)
		}
		values[k] = v
	}
	b, _ = json.Marshal(values)
	path := c.ConfigPath + ".validate"
	if err := os.WriteFile(path, b, 0600); err != nil {
		return Config{}, err
	}
	defer os.Remove(path)
	return LoadConfig(path)
}
func (c *Controller) applyPendingConfig(ctx context.Context) {
	if len(c.State.PendingConfig) == 0 {
		return
	}
	for _, j := range c.State.Jobs {
		if j.Stage == "running" || j.Stage == "publishing" {
			return
		}
	}
	next, err := c.validatedPanelConfig(c.State.PendingConfig)
	if err == nil {
		var b []byte
		b, err = json.MarshalIndent(next, "", "  ")
		if err == nil {
			err = os.WriteFile(c.ConfigPath+".tmp", b, 0600)
		}
		if err == nil {
			err = os.Rename(c.ConfigPath+".tmp", c.ConfigPath)
		}
	}
	if err != nil {
		c.reportPanelIssue(ctx, "Configuration update", err)
		return
	}
	c.C = next
	c.Runner.C = next
	c.API.C = next
	c.State.PendingConfig = nil
	c.panelEvent("configuration", "", "Configuration applied while workers idle")
}
