package netretry

import "strings"

// Temporary recognizes transport failures, never credentials or policy failures.
func Temporary(message string) bool {
	m := strings.ToLower(message)
	for _, permanent := range []string{"authentication failed", "permission denied", "repository not found", "could not read username", "certificate verify failed", "ssl certificate problem"} {
		if strings.Contains(m, permanent) {
			return false
		}
	}
	for _, temporary := range []string{"connection was reset", "connection reset by peer", "could not resolve host", "couldn't resolve host", "failed to connect", "connection timed out", "operation timed out", "remote end hung up unexpectedly", "early eof", "http/2 stream", "http 502", "http 503", "http 504", "requested url returned error: 502", "requested url returned error: 503", "requested url returned error: 504"} {
		if strings.Contains(m, temporary) {
			return true
		}
	}
	return false
}

// ReadFailure limits whole-task retries to trusted, read-only Git preparation.
// Publication has side effects and must never be replayed from diagnostic text.
func ReadFailure(message string) bool {
	return (strings.Contains(message, "trusted Git operation failed: fetch:") || strings.Contains(message, "trusted Git operation failed: ls-remote:")) && Temporary(message)
}
