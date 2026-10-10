package system

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAgentLogRedactionAndReasoningExclusion(t *testing.T) {
	for _, line := range []string{
		`{"type":"response_item","payload":{"type":"reasoning","summary":[{"text":"private thought"}]}}`,
		`{"type":"response_item","payload":{"type":"message","role":"assistant","channel":"analysis","content":[{"type":"output_text","text":"private thought"}]}}`,
		`{"type":"event_msg","payload":{"type":"agent_reasoning","text":"private thought"}}`,
		`{"type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"private user prompt"}]}}`,
	} {
		if _, ok := extractAgentLog([]byte(line), nil); ok {
			t.Fatal("private or input content exposed")
		}
	}
	line := `{"timestamp":"2026-10-10T12:00:00Z","type":"response_item","payload":{"type":"function_call_output","output":"API_KEY=secretvalue Authorization: Bearer opaquevalue known-credential"}}`
	entry, ok := extractAgentLog([]byte(line), []string{"known-credential"})
	if !ok || strings.Contains(entry.Text, "secretvalue") || strings.Contains(entry.Text, "opaquevalue") || strings.Contains(entry.Text, "known-credential") {
		t.Fatal(entry)
	}
	public := `{"type":"response_item","payload":{"type":"message","role":"assistant","channel":"commentary","content":[{"type":"output_text","text":"Running tests"}]}}`
	if entry, ok := extractAgentLog([]byte(public), nil); !ok || !strings.Contains(entry.Text, "Running tests") {
		t.Fatal("public progress excluded")
	}
}

func TestAgentLogsMatchTaskWorkspaceAndRejectTraversal(t *testing.T) {
	dir := t.TempDir()
	native := filepath.Join(dir, "native")
	sessions := filepath.Join(native, "codex-runtime", "sessions")
	os.MkdirAll(sessions, 0700)
	configPath := filepath.Join(dir, "config.json")
	b, _ := json.Marshal(Config{Backend: "native-windows", NativeRoot: native})
	os.WriteFile(configPath, b, 0600)
	state := &State{Jobs: map[string]*Job{"task": {ID: "task", ExecutorSession: "correct", Stage: "closed"}}}
	state.Save(dir)
	meta := func(session, cwd string) string {
		b, _ := json.Marshal(map[string]any{"type": "session_meta", "payload": map[string]string{"id": session, "cwd": cwd}})
		return string(b) + "\n"
	}
	output := `{"type":"response_item","payload":{"type":"function_call_output","output":"test passed"}}` + "\n"
	os.WriteFile(filepath.Join(sessions, "rollout-correct.jsonl"), []byte(meta("correct", filepath.Join(native, "jobs", "task", "workspace"))+output), 0600)
	logs, err := readAgentLogs(configPath, dir, "task", "executor")
	if err != nil || len(logs.Entries) != 1 || logs.Session != "correct" {
		t.Fatal(logs, err)
	}
	os.WriteFile(filepath.Join(sessions, "rollout-correct.jsonl"), []byte(meta("correct", filepath.Join(native, "jobs", "other", "workspace"))+output), 0600)
	logs, err = readAgentLogs(configPath, dir, "task", "executor")
	if err != nil || len(logs.Entries) != 0 {
		t.Fatal("cross-task logs exposed")
	}
	if _, err := readAgentLogs(configPath, dir, "../secret", "executor"); err == nil {
		t.Fatal("path traversal accepted")
	}
}

func TestQuotedLogSecretsAreRedacted(t *testing.T) {
	for _, text := range []string{`password="very private value"`, `{"api_key":"very private value"}`, `GH_TOKEN='very private value'`} {
		if strings.Contains(scrubAgentLog(text, nil), "private") {
			t.Fatal("quoted secret leaked")
		}
	}
}

func TestCustomCodexToolEventsAreVisible(t *testing.T) {
	for _, line := range []string{
		`{"type":"response_item","payload":{"type":"custom_tool_call","name":"exec_command","input":"run tests"}}`,
		`{"type":"response_item","payload":{"type":"custom_tool_call_output","output":"tests passed"}}`,
		`{"type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"Completed tests"}]}}`,
	} {
		if entry, ok := extractAgentLog([]byte(line), nil); !ok || entry.Text == "" {
			t.Fatal("Codex event missing", line)
		}
	}
}
