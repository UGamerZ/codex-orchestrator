package system

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func inputFromProfile(p EnvironmentProfile) *EnvironmentInput {
	c := p.Config
	return &EnvironmentInput{Name: p.Name, Mode: c.ProjectMode, Repository: c.Repository, LocalPath: c.LocalRepository, Branch: c.BaseBranch, Account: c.GithubUser, Board: c.Board, Todo: c.Todo, Progress: c.Progress, Done: c.Done}
}
func TestEnvironmentEditRetainsActiveTasksAndAppliesRuntimeSettings(t *testing.T) {
	c := environmentFixture(t)
	c.State.Jobs["work"] = &Job{ID: "work", Stage: "waiting", ExecutorSession: "keep-session"}
	r, _ := readRegistry(c.ConfigPath)
	input := inputFromProfile(r.Profiles[0])
	input.Name = "Renamed"
	input.Repository = strings.ToUpper(input.Repository)
	input.Account = strings.ToUpper(input.Account)
	patch := map[string]json.RawMessage{"max_workers": json.RawMessage("1"), "reviewer_model": json.RawMessage(`"gpt-6-sol"`)}
	if err := c.updateEnvironment("default", input, patch); err != nil {
		t.Fatal(err)
	}
	r, _ = readRegistry(c.ConfigPath)
	if r.Active != "default" || len(r.Profiles) != 1 || r.Profiles[0].Name != "Renamed" || c.C.MaxWorkers != 1 || c.API.C.ReviewerModel != "gpt-6-sol" || c.State.Jobs["work"].ExecutorSession != "keep-session" {
		t.Fatal("ordinary edit lost state or settings")
	}
	stored, err := LoadConfig(c.ConfigPath)
	if err != nil || stored.MaxWorkers != 1 {
		t.Fatal("active config not saved", err)
	}
}
func TestInactiveEnvironmentProjectEditRetainsPreviousSetup(t *testing.T) {
	c := environmentFixture(t)
	r, _ := readRegistry(c.ConfigPath)
	old := EnvironmentProfile{ID: "other", Name: "Other", Config: c.C}
	old.Config.StateDir = t.TempDir()
	r.Profiles = append(r.Profiles, old)
	writePrivateJSON(registryPath(c.ConfigPath), r)
	state := &State{Jobs: map[string]*Job{"original": {ID: "original", Stage: "waiting", ExecutorSession: "session-old"}}}
	state.Save(old.Config.StateDir)
	before, _ := os.ReadFile(filepath.Join(old.Config.StateDir, "state.json"))
	input := inputFromProfile(old)
	input.Repository = "second/project"
	input.Branch = "release"
	input.Account = "account-two"
	if err := c.updateEnvironment(old.ID, input, nil); err != nil {
		t.Fatal(err)
	}
	r, _ = readRegistry(c.ConfigPath)
	if r.Active != "default" || len(r.Profiles) != 3 {
		t.Fatal("edit switched or lost previous setup")
	}
	next, previous := r.Profiles[1], r.Profiles[2]
	if next.ID != old.ID || next.Config.StateDir == old.Config.StateDir || next.Config.Repository != "second/project" || previous.Config != old.Config || !strings.HasPrefix(previous.ID, "previous-") {
		t.Fatal("project state mixed or original config lost")
	}
	after, _ := os.ReadFile(filepath.Join(old.Config.StateDir, "state.json"))
	if string(after) != string(before) {
		t.Fatal("old state modified")
	}
	loaded, _ := LoadState(next.Config.StateDir)
	if len(loaded.Jobs) != 0 {
		t.Fatal("new setup reused old tasks")
	}
}
func TestEnvironmentEditRejectsInvalidInputsAndActiveWorkers(t *testing.T) {
	c := environmentFixture(t)
	r, _ := readRegistry(c.ConfigPath)
	input := inputFromProfile(r.Profiles[0])
	before, _ := os.ReadFile(registryPath(c.ConfigPath))
	c.State.Jobs["active"] = &Job{Stage: "running"}
	if err := c.updateEnvironment("default", input, nil); err == nil {
		t.Fatal("active worker edit accepted")
	}
	delete(c.State.Jobs, "active")
	input.Todo = "invalid"
	if err := c.updateEnvironment("default", input, nil); err == nil {
		t.Fatal("invalid list accepted")
	}
	input.Todo = c.C.Todo
	if err := c.updateEnvironment("default", input, map[string]json.RawMessage{"native_root": json.RawMessage(`"unsafe"`)}); err == nil {
		t.Fatal("isolation edit accepted")
	}
	after, _ := os.ReadFile(registryPath(c.ConfigPath))
	if string(after) != string(before) {
		t.Fatal("rejected edit changed registry")
	}
}
func TestActiveProjectEditRollsBackWhenAccessCheckFails(t *testing.T) {
	c := environmentFixture(t)
	c.C.GHPath = filepath.Join(t.TempDir(), "missing-gh")
	c.State.Jobs["retained"] = &Job{ID: "retained", Stage: "waiting"}
	writePrivateJSON(c.ConfigPath, c.C)
	c.saveProfileConfiguration(c.C)
	r, _ := readRegistry(c.ConfigPath)
	input := inputFromProfile(r.Profiles[0])
	input.Repository = "inaccessible/project"
	registryBefore, _ := os.ReadFile(registryPath(c.ConfigPath))
	configBefore, _ := os.ReadFile(c.ConfigPath)
	oldDir := c.C.StateDir
	if err := c.updateEnvironment("default", input, nil); err == nil {
		t.Fatal("inaccessible project edit accepted")
	}
	registryAfter, _ := os.ReadFile(registryPath(c.ConfigPath))
	configAfter, _ := os.ReadFile(c.ConfigPath)
	if string(registryBefore) != string(registryAfter) || string(configBefore) != string(configAfter) || c.C.StateDir != oldDir || c.State.Jobs["retained"] == nil {
		t.Fatal("failed edit lost original setup")
	}
}
func TestEditingUnusedSetupStillPreservesPreviousConfiguration(t *testing.T) {
	c := environmentFixture(t)
	r, _ := readRegistry(c.ConfigPath)
	old := EnvironmentProfile{ID: "unused", Name: "Unused", Config: c.C}
	old.Config.StateDir = t.TempDir()
	r.Profiles = append(r.Profiles, old)
	writePrivateJSON(registryPath(c.ConfigPath), r)
	input := inputFromProfile(old)
	input.Todo = strings.Repeat("9", 24)
	if err := c.updateEnvironment(old.ID, input, nil); err != nil {
		t.Fatal(err)
	}
	r, _ = readRegistry(c.ConfigPath)
	if len(r.Profiles) != 3 || r.Profiles[2].Config.Todo != old.Config.Todo {
		t.Fatal("unused previous setup lost")
	}
}
