package system

import "encoding/json"

func blockedRemindersEnabled(c Config) bool {
	return c.BlockedReminders == nil || *c.BlockedReminders
}

func (c *Controller) setBlockedReminders(enabled bool) error {
	next := c.C
	next.BlockedReminders = &enabled
	if err := writePrivateJSON(c.ConfigPath, next); err != nil {
		return err
	}
	if err := c.saveProfileConfiguration(next); err != nil {
		return err
	}
	c.C = next
	c.API.C = next
	c.Runner.C = next
	// A queued model/configuration change must not undo this immediate toggle.
	if len(c.State.PendingConfig) > 0 {
		c.State.PendingConfig["blocked_reminders_enabled"] = json.RawMessage("false")
		if enabled {
			c.State.PendingConfig["blocked_reminders_enabled"] = json.RawMessage("true")
		}
	}
	c.panelEvent("notifications", "", "Telegram blocked-task reminders: "+map[bool]string{true: "enabled", false: "disabled"}[enabled])
	return nil
}
