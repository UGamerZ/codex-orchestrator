package system

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func nativeFixture(t *testing.T) Config {
	root := t.TempDir()
	return Config{Backend: "native-windows", NativeRoot: root, CodexPath: filepath.Join(root, "tools", "codex.exe"), StateDir: filepath.Join(root, "state"), AuthFile: filepath.Join(root, "auth.json"), WorkerModel: "gpt-6.1-sol", WorkerEffort: "low", FallbackModel: "gpt-6-sol", FallbackEffort: "medium", ManagerModel: "gpt-6-luna", ReviewerModel: "gpt-6-luna", OtherEffort: "low"}
}
func TestNativePathsRejectTraversal(t *testing.T) {
	r := &Runner{C: nativeFixture(t)}
	for _, id := range []string{"../escape", "a/b", "C:\\escape", "", "../../", "--flag"} {
		if _, e := r.nativePaths(id); e == nil {
			t.Errorf("accepted %s", id)
		}
	}
	if _, e := r.nativePaths("task-123"); e != nil {
		t.Fatal(e)
	}
}
func TestNativeNeverUsesFullAccess(t *testing.T) {
	c := nativeFixture(t)
	for _, role := range []string{"executor", "reviewer", "manager"} {
		for _, session := range []string{"", "existing-thread"} {
			args := nativeExecArgs(c, Job{Session: session}, role, filepath.Join(c.NativeRoot, "jobs", "id", "workspace"), filepath.Join(c.NativeRoot, "runtime"), "result.json", "schema.json")
			joined := strings.Join(args, " ")
			if strings.Contains(joined, "danger-full-access") || strings.Contains(joined, "bypass-approvals") {
				t.Fatal("native full access")
			}
			for _, required := range []string{"approval_policy=\"never\"", "windows.sandbox=\"elevated\"", "network.enabled=false", "--disable apps", "--disable multi_agent", "--ignore-user-config"} {
				if !strings.Contains(joined, required) {
					t.Errorf("missing %s", required)
				}
			}
			if role != "executor" && strings.Contains(joined, filepath.ToSlash(filepath.Join(c.NativeRoot, "jobs", "id", "workspace"))+"\"=\"write\"") {
				t.Fatal("review/manager workspace write")
			}
		}
	}
}
func TestNativeDoesNotInheritIntegrationSecrets(t *testing.T) {
	for _, key := range []string{"GH_TOKEN", "GITHUB_TOKEN", "TRELLO_TOKEN", "TELEGRAM_TOKEN", "OPENAI_API_KEY", "HTTPS_PROXY"} {
		t.Setenv(key, "private-value")
	}
	env := strings.Join(nativeEnvironment(t.TempDir()), "\n")
	if strings.Contains(env, "private-value") {
		t.Fatal("inherited service secret")
	}
}
func TestNativeAuthRejectsAPIKey(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source.json")
	os.WriteFile(source, []byte(`{"OPENAI_API_KEY":"secret","tokens":null}`), 0600)
	if prepareNativeAuth(source, filepath.Join(root, "runtime")) == nil {
		t.Fatal("API key accepted")
	}
}
