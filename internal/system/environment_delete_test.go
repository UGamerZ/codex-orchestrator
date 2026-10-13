package system

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func deletionFixture(t *testing.T) (*Controller, EnvironmentProfile) {
	t.Helper()
	c := environmentFixture(t)
	r, err := readRegistry(c.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	config := c.C
	config.StateDir = t.TempDir()
	p := EnvironmentProfile{ID: "saved", Name: "Saved", Config: config}
	r.Profiles = append(r.Profiles, p)
	if err = writePrivateJSON(registryPath(c.ConfigPath), r); err != nil {
		t.Fatal(err)
	}
	state := &State{Jobs: map[string]*Job{"task": {ID: "task", Card: strings.Repeat("a", 24), PR: 16, Stage: "waiting", QuestionMessage: 42, CompletionMessages: []int64{55}, ExecutorSession: "keep"}}}
	if err = state.Save(config.StateDir); err != nil {
		t.Fatal(err)
	}
	return c, p
}

func TestEnvironmentDeletionRetainsFilesAndPullOwnership(t *testing.T) {
	c, p := deletionFixture(t)
	statePath := filepath.Join(p.Config.StateDir, "state.json")
	before, _ := os.ReadFile(statePath)
	configBefore, _ := os.ReadFile(c.ConfigPath)
	if err := c.executePanelCommand(PanelCommand{Action: "environment-delete", ID: p.ID}); err != nil {
		t.Fatal(err)
	}
	r, _ := readRegistry(c.ConfigPath)
	if r.Active != "default" || !r.Profiles[1].Deleted {
		t.Fatal("wrong environment removed")
	}
	after, _ := os.ReadFile(statePath)
	configAfter, _ := os.ReadFile(c.ConfigPath)
	if string(before) != string(after) || string(configBefore) != string(configAfter) {
		t.Fatal("deletion altered tasks or active configuration")
	}
	profiles, err := publicEnvironmentProfiles(c.ConfigPath)
	if err != nil || len(profiles["profiles"].([]map[string]any)) != 1 {
		t.Fatal("deleted profile still shown", err)
	}
	if err := c.switchEnvironment(p.ID); err == nil {
		t.Fatal("deleted environment activated")
	}
	input := &EnvironmentInput{Name: "Renamed"}
	if err := c.updateEnvironment(p.ID, input, nil); err == nil {
		t.Fatal("deleted environment edited")
	}
	owned, card, err := c.inactivePullOwner(Pull{Number: 16})
	if err != nil || !owned || card != strings.Repeat("a", 24) {
		t.Fatal("deleted task lost PR ownership", err)
	}
	input = &EnvironmentInput{Name: p.Name, Mode: "github", Repository: c.C.Repository, Branch: c.C.BaseBranch, Account: c.C.GithubUser, Board: c.C.Board, Todo: c.C.Todo, Progress: c.C.Progress, Done: c.C.Done}
	if err := c.createEnvironment(input); err != nil {
		t.Fatal("deleted name cannot be reused", err)
	}
}

func TestEnvironmentDeletionRejectsActiveLockedOrRunningProfiles(t *testing.T) {
	for _, kind := range []string{"active", "shared-state", "locked", "running", "publishing", "invalid-state", "invalid-id", "unknown"} {
		t.Run(kind, func(t *testing.T) {
			c, p := deletionFixture(t)
			id := p.ID
			statePath := filepath.Join(p.Config.StateDir, "state.json")
			switch kind {
			case "active":
				id = "default"
			case "shared-state":
				r, _ := readRegistry(c.ConfigPath)
				r.Profiles[1].Config.StateDir = c.C.StateDir
				writePrivateJSON(registryPath(c.ConfigPath), r)
			case "locked":
				os.WriteFile(filepath.Join(p.Config.StateDir, "controller.lock"), []byte("live"), 0600)
			case "running", "publishing":
				(&State{Jobs: map[string]*Job{"task": {ID: "task", Stage: kind}}}).Save(p.Config.StateDir)
			case "invalid-state":
				os.WriteFile(statePath, []byte("invalid"), 0600)
			case "invalid-id":
				id = "../saved"
			case "unknown":
				id = "unknown"
			}
			before, _ := os.ReadFile(registryPath(c.ConfigPath))
			if err := c.deleteEnvironment(id); err == nil {
				t.Fatal("unsafe deletion accepted")
			}
			after, _ := os.ReadFile(registryPath(c.ConfigPath))
			if string(before) != string(after) {
				t.Fatal("failed deletion changed registry")
			}
			if kind == "locked" {
				b, _ := os.ReadFile(filepath.Join(p.Config.StateDir, "controller.lock"))
				if string(b) != "live" {
					t.Fatal("existing lock altered")
				}
			} else if _, err := os.Stat(filepath.Join(p.Config.StateDir, "controller.lock")); !os.IsNotExist(err) {
				t.Fatal("temporary lock leaked")
			}
		})
	}
}

func TestDeletedEnvironmentRepliesCannotRestartAgents(t *testing.T) {
	c, p := deletionFixture(t)
	if err := c.deleteEnvironment(p.ID); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(p.Config.StateDir, "state.json")
	before, _ := os.ReadFile(path)
	c.API.Client.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
		if !strings.HasSuffix(r.URL.Path, "/sendMessage") {
			t.Fatal("unexpected mutation")
		}
		return response(`{"ok":true,"result":{"message_id":100}}`), nil
	})
	for _, message := range []int64{42, 55} {
		m := &TGMessage{Text: "Retry", Reply: &TGMessage{ID: message}}
		m.Chat.ID = c.C.Chat
		if handled, err := c.routeInactiveAnswer(context.Background(), m); !handled || err != nil {
			t.Fatal("deleted reply not handled", err)
		}
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Fatal("deleted task restarted or altered")
	}
}
