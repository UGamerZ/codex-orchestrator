package system

import "strings"

// A human reply/retry to an exhausted correction cycle authorizes a new budget.
// Technical retries and ordinary agent turns must not reset this counter.
func renewFixBudget(j *Job) {
	if strings.HasPrefix(j.Question, "The automatic fix limit has been reached.") {
		j.Fixes = 0
	}
}
