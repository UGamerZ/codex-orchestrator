package system

import "strings"

func publicationTitle(j *Job) string {
	title := j.CommitTitle
	if strings.TrimSpace(title) == "" {
		// A pending publication saved by an older controller has no commit_title.
		title = j.Summary
	}
	title = strings.Join(strings.Fields(strings.ReplaceAll(title, "\x00", "")), " ")
	if title == "" {
		title = "Update task implementation"
	}
	runes := []rune(title)
	if len(runes) > 100 {
		title = string(runes[:100])
	}
	return title
}
