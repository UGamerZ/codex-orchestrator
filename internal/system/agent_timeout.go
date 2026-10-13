package system

import "time"

// Reviews have their own explicit deadline; silence alone is never a timeout.
func agentRunTimeout(c Config, role string) time.Duration {
	minutes := c.TimeoutMinutes
	if role == "reviewer" {
		review := c.ReviewTimeout
		if review == 0 {
			review = 15
		}
		if review < minutes {
			minutes = review
		}
	}
	return time.Duration(minutes) * time.Minute
}
