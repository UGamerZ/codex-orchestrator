package system

import "time"

// Legacy provenance comes from trusted task state, not the PR title/branch name.
// PRs discovered externally never have both a matching Trello card and executor session.
func systemCreatedTask(j *Job) bool {
	return j.SystemCreated || (j.Card == j.ID && j.Card != "" && j.Branch == "codex/"+j.ID && j.ExecutorSession != "")
}

func adoptTaskHead(j *Job, head string) {
	j.SystemCreated = true
	j.External = false
	j.Head = head
	j.BaseSHA = ""
	j.ReviewedHead = ""
	j.ReviewerSession = ""
	j.ExecutorSession = ""
	j.Session = ""
	j.Question = ""
	j.QuestionMessage = 0
	j.LastError = ""
	j.RetryAt = time.Time{}
	j.Fixes = 0
	j.Role = "reviewer"
	j.Stage = "review"
}
