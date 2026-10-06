//go:build !windows

package system

import "fmt"

func NativeNetworkPolicy(install bool) error {
	return fmt.Errorf("native network policy requires Windows")
}
