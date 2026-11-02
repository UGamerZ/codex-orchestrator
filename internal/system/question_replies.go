package system

import (
	"context"
	"fmt"
	"time"
)

const legacyReminderFooter = "Reply to the original question or use the dashboard."
const reminderFooter = "Use Reply on this message to answer the waiting agent or retry this system blocker. Your answer stays with this task. Do not include credentials or tokens."

func blockedReminderText(j *Job, footer string) string {
	return "Task still blocked [" + j.ID + "]: " + j.Title + "\n\n" + j.Question + "\n\n" + footer
}

func (c *Controller) sendBlockedReminder(ctx context.Context, j *Job) error {
	id, err := c.API.Send(ctx, blockedReminderText(j, reminderFooter))
	if err != nil {
		return err
	}
	if j.QuestionReminderRoot != j.QuestionMessage {
		j.QuestionReminderRoot = j.QuestionMessage
		j.QuestionReminders = nil
	}
	if id != 0 {
		j.QuestionReminders = append(j.QuestionReminders, id)
	}
	j.BlockedReminder = time.Now().UTC()
	return nil
}

func questionReply(j *Job, reply *TGMessage) bool {
	if reply == nil || reply.ID == 0 || j.QuestionMessage == 0 {
		return false
	}
	if reply.ID == j.QuestionMessage {
		return true
	}
	if j.QuestionReminderRoot == j.QuestionMessage {
		for _, id := range j.QuestionReminders {
			if id == reply.ID {
				return true
			}
		}
	}
	// Older reminders did not retain IDs. Telegram supplies the original bot
	// message in reply_to_message. Match the full CURRENT question, not just a
	// task ID, so an old answer cannot silently answer a different blocker.
	if reply.From == nil || !reply.From.IsBot || j.Question == "" {
		return false
	}
	for _, footer := range []string{legacyReminderFooter, reminderFooter} {
		if reply.Text == blockedReminderText(j, footer) {
			return true
		}
	}
	label := "Question from " + j.Role
	if j.QuestionOrigin == "system" {
		label = "System blocker"
	}
	question := fmt.Sprintf("%s [%s]: %s\n\n%s\n\nUse Reply on this message. Your answer is retained for this task. Do not include credentials or tokens; confirm when the required resource is ready.", label, j.ID, j.Title, j.Question)
	if reply.Text == question {
		return true
	}
	if j.QuestionOrigin == "system" && j.LastError != "" {
		issue := "System issue: Task [" + j.ID + "] " + j.Role + "\n" + j.LastError + "\nTasks and known sessions are retained. Check the dashboard for current status."
		if reply.Text == issue {
			return true
		}
	}
	return false
}
