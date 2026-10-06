package system

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"codex-orchestrator/internal/broker"
)

func (r *Runner) nativePaths(id string) (broker.Paths, error) {
	if !safeID.MatchString(id) || r.C.NativeRoot == "" {
		return broker.Paths{}, fmt.Errorf("invalid native task root")
	}
	root, e := filepath.Abs(r.C.NativeRoot)
	if e != nil {
		return broker.Paths{}, e
	}
	base := filepath.Join(root, "jobs", id)
	for _, p := range []string{root, filepath.Join(root, "jobs"), base} {
		if info, e := os.Lstat(p); e == nil && info.Mode()&os.ModeSymlink != 0 {
			return broker.Paths{}, fmt.Errorf("native root cannot contain a reparse point")
		}
	}
	p := broker.Paths{Workspace: filepath.Join(base, "workspace"), Publish: filepath.Join(base, "private", "publish"), Review: filepath.Join(base, "review-input")}
	for _, v := range []string{p.Workspace, p.Publish, p.Review} {
		rel, e := filepath.Rel(root, v)
		if e != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return broker.Paths{}, fmt.Errorf("native path escapes configured root")
		}
		if info, e := os.Lstat(v); e == nil && info.Mode()&os.ModeSymlink != 0 {
			return broker.Paths{}, fmt.Errorf("task path is a reparse point")
		}
	}
	return p, nil
}
func (r *Runner) nativeBroker(ctx context.Context, j *Job, operation string) (broker.Result, error) {
	p, e := r.nativePaths(j.ID)
	if e != nil {
		return broker.Result{}, e
	}
	for _, dir := range []string{p.Workspace, p.Publish, p.Review} {
		if e = os.MkdirAll(dir, 0700); e != nil {
			return broker.Result{}, e
		}
	}
	token, e := exec.CommandContext(ctx, r.C.GHPath, "auth", "token", "--hostname", "github.com", "--user", r.C.GithubUser).Output()
	if e != nil {
		return broker.Result{}, fmt.Errorf("GitHub credential unavailable")
	}
	req := broker.Request{Operation: operation, Repository: r.C.Repository, Base: r.C.BaseBranch, SHA: j.BaseSHA, Branch: j.Branch, Token: strings.TrimSpace(string(token)), Message: clip(j.Title, 200)}
	if operation == "prepare" && j.External {
		req.SHA = j.Head
		req.PR = j.PR
	}
	if operation == "review" {
		req.SHA = j.Head
		req.PR = j.PR
		req.Message = j.Title + "\n" + j.Description
	}
	return broker.Execute(ctx, req, p)
}

// This Codex Windows implementation requires root read as a baseline. Deny known
// non-runtime top-level trees and then reopen only the current task and tool paths.
// A deny on the entire job store is reopened only for this workspace and its scratch dir.
func nativePolicyArgs(c Config, workspace, home, role string) []string {
	access := "read"
	if role == "executor" {
		access = "write"
	}
	fs := map[string]string{":root": "read", c.NativeRoot: "deny", c.StateDir: "deny", c.AuthFile: "deny", home: "deny", workspace: access, filepath.Join(home, "scratch"): "write", filepath.Dir(c.CodexPath): "read"}
	if role == "reviewer" {
		fs[filepath.Join(filepath.Dir(workspace), "review-input")] = "read"
	}
	if runtime.GOOS == "windows" {
		for drive := 'C'; drive <= 'Z'; drive++ {
			root := string(drive) + ":\\"
			entries, e := os.ReadDir(root)
			if e != nil {
				continue
			}
			for _, entry := range entries {
				name := strings.ToLower(entry.Name())
				if name == "windows" || name == "program files" || name == "program files (x86)" || name == "$recycle.bin" || name == "system volume information" {
					continue
				}
				if !entry.IsDir() {
					file, e := os.Open(filepath.Join(root, entry.Name()))
					if e != nil {
						continue
					}
					file.Close()
				}
				fs[filepath.Join(root, entry.Name())] = "deny"
			}
		}
	}
	if ownerHome, e := os.UserHomeDir(); e == nil {
		fs[ownerHome] = "deny"
	}
	if systemRoot := os.Getenv("SystemRoot"); systemRoot != "" {
		fs[systemRoot] = "read"
	}
	// Explicit runtime tools, never the whole XAMPP directory or GitHub credential store.
	for _, program := range []string{"git", "php"} {
		if p, e := exec.LookPath(program); e == nil {
			fs[filepath.Dir(p)] = "read"
		}
	}
	entries := []string{}
	for k, v := range fs {
		entries = append(entries, strconv.Quote(filepath.ToSlash(k))+"="+strconv.Quote(v))
	}
	return []string{"-c", "default_permissions=\"orchestrator\"", "-c", "permissions.orchestrator.filesystem={" + strings.Join(entries, ",") + "}", "-c", "permissions.orchestrator.network.enabled=false", "-c", "approval_policy=\"never\"", "-c", "windows.sandbox=\"elevated\"", "-c", "windows.sandbox_private_desktop=true", "-c", "cli_auth_credentials_store=\"file\""}
}
func nativeEnvironment(home string) []string {
	// Do not inherit a service's tokens, proxy settings, Git configuration or credentials.
	env := []string{}
	for _, key := range []string{"PATH", "SystemRoot", "WINDIR", "COMSPEC", "PATHEXT", "USERPROFILE", "APPDATA", "LOCALAPPDATA", "ProgramFiles", "ProgramData"} {
		if value := os.Getenv(key); value != "" {
			env = append(env, key+"="+value)
		}
	}
	scratch := filepath.Join(home, "scratch")
	return append(env, "CODEX_HOME="+home, "TEMP="+scratch, "TMP="+scratch)
}
func prepareNativeAuth(source, home string) error {
	b, e := os.ReadFile(source)
	if e != nil {
		return fmt.Errorf("Codex authentication unavailable")
	}
	var auth map[string]json.RawMessage
	if e = json.Unmarshal(b, &auth); e != nil {
		return e
	}
	var tokens map[string]string
	if json.Unmarshal(auth["tokens"], &tokens) != nil || tokens["access_token"] == "" {
		return fmt.Errorf("ChatGPT login required")
	}
	clean := map[string]json.RawMessage{}
	for _, k := range []string{"auth_mode", "tokens", "last_refresh"} {
		if value := auth[k]; len(value) > 0 {
			clean[k] = value
		}
	}
	b, e = json.Marshal(clean)
	if e != nil {
		return e
	}
	if e = os.MkdirAll(filepath.Join(home, "scratch"), 0700); e != nil {
		return e
	}
	if info, e := os.Lstat(filepath.Join(home, "auth.json")); e == nil && info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("auth file is a reparse point")
	}
	return os.WriteFile(filepath.Join(home, "auth.json"), b, 0600)
}

var disabledNativeFeatures = []string{"apps", "plugins", "remote_plugin", "hooks", "multi_agent", "multi_agent_v2", "computer_use", "browser_use", "browser_use_external", "in_app_browser", "daemon_auto_start", "workspace_dependencies", "skill_mcp_dependency_install", "skill_search", "image_generation", "tool_suggest", "auth_elicitation", "windows_sandbox_service"}

func nativeExecArgs(c Config, j Job, role, workspace, home, result, schema string) []string {
	args := []string{"exec"}
	if j.Session != "" {
		args = append(args, "resume")
	}
	args = append(args, "--ignore-user-config", "--ignore-rules", "--skip-git-repo-check", "--json")
	args = append(args, nativePolicyArgs(c, workspace, home, role)...)
	for _, feature := range disabledNativeFeatures {
		args = append(args, "--disable", feature)
	}
	model, effort := c.WorkerModel, c.WorkerEffort
	if role == "reviewer" {
		model, effort = c.ReviewerModel, c.OtherEffort
	}
	if role == "manager" {
		model, effort = c.ManagerModel, c.OtherEffort
	}
	if role == "executor" && j.Fallback {
		model, effort = c.FallbackModel, c.FallbackEffort
	}
	args = append(args, "-c", "web_search=\"disabled\"", "-c", "model_reasoning_effort="+strconv.Quote(effort), "--model", model, "--output-schema", schema, "--output-last-message", result)
	if j.Session != "" {
		args = append(args, j.Session)
	}
	return append(args, "-")
}
