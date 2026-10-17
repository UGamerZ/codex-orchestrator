package system

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestColimaPolicyChecksRunInsideSameVMAsWorkers(t *testing.T) {
	r := &Runner{C: Config{DockerVM: "colima"}}
	args := r.dockerCommand(context.Background(), "run", "--rm", "image").Args
	if !reflect.DeepEqual(args, []string{"docker", "--context", "colima-codex-orchestrator", "run", "--rm", "image"}) {
		t.Fatal("worker used a different Docker context", args)
	}
	args = r.hostCommand(context.Background(), "iptables", "-C", "INPUT", "-s", "172.30.250.0/24", "-j", "DROP").Args
	want := []string{"colima", "ssh", "codex-orchestrator", "--", "sudo", "-n", "--", "iptables", "-C", "INPUT", "-s", "172.30.250.0/24", "-j", "DROP"}
	if !reflect.DeepEqual(args, want) {
		t.Fatal("firewall verification must run inside the dedicated VM", args)
	}
}

func TestInvalidDockerVMDoesNotReachCredentialsOrCommands(t *testing.T) {
	for _, config := range []Config{{Backend: "docker", DockerVM: "arbitrary-host"}, {Backend: "docker", DockerVM: "colima", DockerDistro: "Ubuntu"}, {Backend: "native-windows", DockerVM: "colima"}} {
		path := filepath.Join(t.TempDir(), "config.json")
		data, _ := json.Marshal(config)
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadConfig(path); err == nil || !strings.Contains(err.Error(), "docker_vm") {
			t.Fatal("invalid VM selection accepted", err)
		}
	}
}
