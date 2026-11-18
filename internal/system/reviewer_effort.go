package system

// Older configurations shared other_effort between manager and reviewer.
func reviewerEffort(c Config) string {
	if c.ReviewerEffort != "" {
		return c.ReviewerEffort
	}
	return c.OtherEffort
}
