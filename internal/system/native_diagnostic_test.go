package system

import (
	"errors"
	"strings"
	"testing"
)

func TestNativeFailureDiagnosticsRetainCauseAndHidePrivateOutput(t *testing.T) {
	out := `{"type":"item.completed","item":{"type":"reasoning","text":"PRIVATE_THOUGHT"}}` + "\n" +
		`{"type":"item.completed","item":{"type":"agent_message","text":"PRIVATE_ANSWER"}}` + "\n" +
		`{"type":"turn.failed","error":{"message":"permission denied access_token=supersecretvalue"}}`
	message := nativeProcessError(out, "", errors.New("exit status 1"), []string{"supersecretvalue"}).Error()
	if !strings.Contains(message, "permission denied") || !strings.Contains(message, "exit status 1") || !strings.Contains(message, "[REDACTED]") {
		t.Fatal(message)
	}
	for _, private := range []string{"PRIVATE_THOUGHT", "PRIVATE_ANSWER", "supersecretvalue"} {
		if strings.Contains(message, private) {
			t.Fatal("private output leaked")
		}
	}
}
func TestNativeFailureStderrAndUnknownFailures(t *testing.T) {
	message := nativeProcessError("", "Warning: ignored\nError: failed to write result", errors.New("exit status 1"), nil).Error()
	if !strings.Contains(message, "failed to write result") {
		t.Fatal(message)
	}
	message = nativeProcessError(`{"type":"item.completed","item":{"text":"private"}}`, "unclassified private output", errors.New("exit status 2"), nil).Error()
	if strings.Contains(message, "private") {
		t.Fatal("unknown output exposed")
	}
}
