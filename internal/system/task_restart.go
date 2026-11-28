package system

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"
)

func (c *Controller) restartCompletedTask(id string) error {
	old := c.State.Jobs[id]
	if old == nil || old.Card == "" || old.Stage == "running" || old.Stage == "publishing" {
		return fmt.Errorf("close the old PR and stop its workers before starting the task from scratch")
	}
	if !old.ClosedPR && old.PR > 0 {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		pull, err := c.API.Pull(ctx, old.PR)
		if err != nil || pull.State != "closed" || pull.Merged {
			return fmt.Errorf("old PR must be closed without merging before a fresh restart")
		}
		old.ClosedPR = true
	}
	if !old.ClosedPR {
		return fmt.Errorf("old task must have a confirmed closed PR")
	}
	var nonce [6]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return err
	}
	nextID := old.Card + "-r" + hex.EncodeToString(nonce[:])
	if !safeID.MatchString(nextID) || c.State.Jobs[nextID] != nil {
		return fmt.Errorf("fresh task identity is invalid or already exists")
	}
	next := &Job{ID: nextID, Card: old.Card, Title: old.Title, Description: old.Description, Checklists: old.Checklists, Branch: "codex/" + nextID, SystemCreated: true, Stage: "queued", Role: "executor", AddedAt: time.Now().UTC(), FixLimitDisabled: old.FixLimitDisabled}
	next.Answer = "Fresh implementation from the configured base branch, as explicitly requested by the user. Do not copy the previous task implementation or continue its old review findings. Use only the task requirements and explicit human decisions. All executor/reviewer iterations stay local; publish to GitHub only after internal review passes."
	for _, reply := range old.Clarifications {
		if reply.Origin == "agent" || reply.Origin == "human-review" {
			next.Clarifications = append(next.Clarifications, reply)
		}
	}
	if err := c.deleteCompletedTask(id); err != nil {
		return err
	}
	c.State.Jobs[nextID] = next
	c.panelEvent("task-restarted", nextID, "Fresh execution from the configured base branch; previous PR, branch, logs and archived history retained. New GitHub publication waits for internal review.")
	return nil
}
