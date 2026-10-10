package system

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

type AgentLogEntry struct {
	At   string `json:"at"`
	Kind string `json:"kind"`
	Text string `json:"text"`
}
type AgentLogs struct {
	Session string          `json:"session"`
	Entries []AgentLogEntry `json:"entries"`
	Notice  string          `json:"notice"`
}

var logCredential = regexp.MustCompile(`(?i)(password|passwd|api[_-]?key|access[_-]?token|refresh[_-]?token|id[_-]?token|authorization|secret|gh_token|telegram_token)(["']?\s*[:=]\s*["']?)([^\s,;"'\\]+)`)
var logQuotedCredential = regexp.MustCompile(`(?i)(password|passwd|api[_-]?key|access[_-]?token|refresh[_-]?token|id[_-]?token|authorization|secret|gh_token|telegram_token)(["']?\s*[:=]\s*)(?:"[^"]*"|'[^']*')`)
var logBearer = regexp.MustCompile(`(?i)\bbearer\s+[A-Za-z0-9._~+/-]+=*`)
var logToken = regexp.MustCompile(`(?:github_pat_[A-Za-z0-9_]+|gh[pousr]_[A-Za-z0-9]+|sk-[A-Za-z0-9_-]+|https://api\.telegram\.org/bot[^/\s]+)`)

func scrubAgentLog(text string, secrets []string) string {
	for _, secret := range secrets {
		if len(secret) >= 8 {
			text = strings.ReplaceAll(text, secret, "[REDACTED]")
		}
	}
	text = logBearer.ReplaceAllString(text, "Bearer [REDACTED]")
	text = logToken.ReplaceAllString(text, "[REDACTED]")
	text = logQuotedCredential.ReplaceAllString(text, "$1$2[REDACTED]")
	text = logCredential.ReplaceAllString(text, "$1$2[REDACTED]")
	return clip(text, 8000)
}
func agentLogSecrets(configPath string, c Config) []string {
	var result []string
	var collect func(any)
	collect = func(v any) {
		switch x := v.(type) {
		case string:
			if len(x) >= 8 {
				result = append(result, x)
			}
		case map[string]any:
			for _, child := range x {
				collect(child)
			}
		case []any:
			for _, child := range x {
				collect(child)
			}
		}
	}
	for _, path := range []string{filepath.Join(filepath.Dir(configPath), "secrets", "integrations.json"), c.AuthFile} {
		b, e := os.ReadFile(path)
		if e != nil {
			continue
		}
		var v any
		if json.Unmarshal(b, &v) == nil {
			collect(v)
		}
	}
	sort.Slice(result, func(i, j int) bool { return len(result[i]) > len(result[j]) })
	return result
}
func extractAgentLog(line []byte, secrets []string) (AgentLogEntry, bool) {
	var event struct {
		At      string `json:"timestamp"`
		Type    string `json:"type"`
		Payload struct {
			Type      string          `json:"type"`
			Role      string          `json:"role"`
			Channel   string          `json:"channel"`
			Name      string          `json:"name"`
			Input     string          `json:"input"`
			Arguments string          `json:"arguments"`
			Output    json.RawMessage `json:"output"`
			Message   string          `json:"message"`
			Content   []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"payload"`
	}
	if json.Unmarshal(line, &event) != nil {
		return AgentLogEntry{}, false
	}
	p := event.Payload
	kind, text := "", ""
	switch {
	case event.Type == "response_item" && (p.Type == "function_call" || p.Type == "custom_tool_call"):
		kind = "tool call"
		text = p.Name + "\n" + p.Arguments + p.Input
	case event.Type == "response_item" && (p.Type == "function_call_output" || p.Type == "custom_tool_call_output"):
		kind = "tool output"
		if json.Unmarshal(p.Output, &text) != nil {
			text = string(p.Output)
		}
	case event.Type == "response_item" && p.Type == "message" && p.Role == "assistant" && (p.Channel == "commentary" || p.Channel == "final" || p.Channel == ""):
		kind = p.Channel
		if kind == "" {
			kind = "agent message"
		}
		for _, part := range p.Content {
			if part.Type == "output_text" {
				text += part.Text + "\n"
			}
		}
	case event.Type == "event_msg" && p.Type == "error":
		kind = "error"
		text = p.Message
	default:
		return AgentLogEntry{}, false
	}
	// Suppress credential-file reads rather than relying solely on token patterns.
	lowered := strings.ToLower(text)
	for _, marker := range []string{"auth.json", "integrations.json", "refresh_token", "id_token"} {
		if strings.Contains(lowered, marker) {
			text = "[Entry suppressed: credential-related content]"
			break
		}
	}
	return AgentLogEntry{event.At, kind, scrubAgentLog(text, secrets)}, strings.TrimSpace(text) != ""
}

func readAgentLogs(configPath, stateDir, id, role string) (AgentLogs, error) {
	result := AgentLogs{Entries: []AgentLogEntry{}, Notice: "Recent bounded logs; private reasoning and credential-related entries are excluded."}
	if !safeID.MatchString(id) || (role != "executor" && role != "reviewer" && role != "manager") {
		return result, fmt.Errorf("invalid task or role")
	}
	b, e := os.ReadFile(configPath)
	if e != nil {
		return result, e
	}
	var c Config
	if json.Unmarshal(b, &c) != nil {
		return result, fmt.Errorf("configuration unavailable")
	}
	b, e = os.ReadFile(filepath.Join(stateDir, "state.json"))
	if e != nil {
		return result, e
	}
	var state State
	if json.Unmarshal(b, &state) != nil {
		return result, fmt.Errorf("state unavailable")
	}
	j := state.Jobs[id]
	if j == nil {
		return result, fmt.Errorf("task unavailable")
	}
	if c.Backend != "native-windows" {
		result.Notice = "Detailed native session logs are unavailable for this backend; use the controller event history."
		return result, nil
	}
	session := j.ExecutorSession
	if role == "reviewer" {
		session = j.ReviewerSession
	}
	if role == "manager" {
		session = j.Session
	}
	type candidate struct {
		path string
		time int64
	}
	var files []candidate
	filepath.WalkDir(filepath.Join(c.NativeRoot, "codex-runtime", "sessions"), func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !d.IsDir() && strings.HasSuffix(d.Name(), ".jsonl") {
			if session == "" || strings.Contains(d.Name(), session) {
				info, e := d.Info()
				if e == nil {
					files = append(files, candidate{path, info.ModTime().UnixNano()})
				}
			}
		}
		return nil
	})
	sort.Slice(files, func(i, j int) bool { return files[i].time > files[j].time })
	if session == "" && (j.Stage != "running" || j.Role != role) {
		result.Notice = "No recorded session for this task and role."
		return result, nil
	}
	for _, file := range files {
		f, e := os.Open(file.path)
		if e != nil {
			continue
		}
		first, e := bufio.NewReader(io.LimitReader(f, 1<<20)).ReadBytes('\n')
		if e != nil {
			f.Close()
			continue
		}
		var meta struct {
			Payload struct {
				ID  string `json:"id"`
				CWD string `json:"cwd"`
			} `json:"payload"`
		}
		if json.Unmarshal(first, &meta) != nil {
			f.Close()
			continue
		}
		want := filepath.Join(c.NativeRoot, "jobs", id, "workspace")
		if !strings.EqualFold(filepath.Clean(meta.Payload.CWD), filepath.Clean(want)) || (session != "" && meta.Payload.ID != session) {
			f.Close()
			continue
		}
		result.Session = meta.Payload.ID
		info, e := f.Stat()
		if e != nil {
			f.Close()
			return result, e
		}
		offset := info.Size() - (1 << 20)
		if offset < 0 {
			offset = 0
		}
		f.Seek(offset, io.SeekStart)
		scanner := bufio.NewScanner(io.LimitReader(f, 1<<20))
		scanner.Buffer(make([]byte, 4096), 1<<20)
		secrets := agentLogSecrets(configPath, c)
		for scanner.Scan() {
			if entry, ok := extractAgentLog(scanner.Bytes(), secrets); ok {
				result.Entries = append(result.Entries, entry)
				if len(result.Entries) > 300 {
					result.Entries = result.Entries[len(result.Entries)-300:]
				}
			}
		}
		f.Close()
		return result, nil
	}
	result.Notice = "No session log is available for this task and role."
	return result, nil
}
