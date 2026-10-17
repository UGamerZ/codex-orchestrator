//go:build !windows

package system

import "path/filepath"

func prepareNativeProvisioning(c Config, root string) error {
	seed := nativeProvisioningRoot(c)
	for _, relative := range []string{filepath.Join(".sandbox", "setup_marker.json"), filepath.Join(".sandbox-secrets", "sandbox_users.json")} {
		if err := copyNativeProvisioningFile(filepath.Join(seed, relative), filepath.Join(root, relative)); err != nil {
			return err
		}
	}
	return nil
}
