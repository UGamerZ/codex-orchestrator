//go:build !windows

package system

import (
	"context"
	"fmt"
)

func VerifyNativeIsolation(ctx context.Context, c Config) error {
	return fmt.Errorf("native isolation probe requires Windows")
}
