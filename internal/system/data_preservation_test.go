package system

import (
	"strings"
	"testing"
)

func TestEveryRoleReceivesDataPreservationPolicy(t *testing.T) {
	c := fixture(t)
	for _, role := range []string{"manager", "executor", "reviewer"} {
		prompt := c.prompt(Job{}, role)
		if !strings.HasPrefix(prompt, dataPreservationPolicy) {
			t.Fatal(role + " lacks data preservation policy")
		}
	}
}
