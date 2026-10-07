package broker

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

func gitOutput(ctx context.Context, operation, token string, command func() *exec.Cmd) ([]byte, error) {
	attempts := 1
	if operation == "push" {
		attempts = 3
	}
	var err error
	for attempt := 0; attempt < attempts; attempt++ {
		var b []byte
		b, err = command().Output()
		if err == nil {
			return b, nil
		}
		if ctx.Err() != nil {
			return nil, fmt.Errorf("trusted Git operation failed: %s: request cancelled", operation)
		}
		if attempt+1 < attempts {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(2 * time.Second):
			}
		}
	}
	detail := ""
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		detail = string(exit.Stderr)
	}
	return nil, fmt.Errorf("trusted Git operation failed: %s: %s", operation, safeGitDiagnostic(detail, token))
}

func safeGitDiagnostic(detail, token string) string {
	if token != "" {
		for _, secret := range []string{token, base64.StdEncoding.EncodeToString([]byte("x-access-token:" + token)), base64.StdEncoding.EncodeToString([]byte(token))} {
			detail = strings.ReplaceAll(detail, secret, "[redacted]")
		}
	}
	detail = strings.Join(strings.Fields(detail), " ")
	if detail == "" {
		return "no additional diagnostic available"
	}
	runes := []rune(detail)
	if len(runes) > 1000 {
		detail = string(runes[:1000]) + "..."
	}
	return detail
}
