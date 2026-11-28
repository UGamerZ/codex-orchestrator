package broker

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
)

// Kept in the trusted publisher directory, outside every agent's access.
type checkpointReceipt struct {
	SHA, Branch, ExpectedHead, Baseline string
}

func writeCheckpoint(root string, receipt checkpointReceipt) error {
	b, err := json.Marshal(receipt)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(root, "review-checkpoint.json"), b, 0600)
}

func readCheckpoint(root, sha, branch string) (checkpointReceipt, error) {
	var receipt checkpointReceipt
	b, err := os.ReadFile(filepath.Join(root, "review-checkpoint.json"))
	if err != nil || json.Unmarshal(b, &receipt) != nil || receipt.SHA != sha || receipt.Branch != branch || !regexp.MustCompile(`^[a-f0-9]{40}$`).MatchString(sha) {
		return receipt, fmt.Errorf("trusted review checkpoint is missing or does not match")
	}
	return receipt, nil
}
