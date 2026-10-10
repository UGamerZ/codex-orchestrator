package system

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPanelRejectsCrossOriginAndRedactsCredentials(t *testing.T) {
	dir := t.TempDir()
	config := filepath.Join(dir, "config.json")
	os.WriteFile(config, []byte(`{"worker_model":"test","codex_auth_file":"secret-path","telegram_chat":123}`), 0600)
	state := &State{Jobs: map[string]*Job{"task": {ID: "task", Answer: "private answer", Clarifications: []Clarification{{Answer: "secret reply"}}}}}
	state.Save(dir)
	handler := panelHandler(config, dir, "test-token", make(chan PanelCommand, 1))
	request := func(path, host, token, origin string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", path, nil)
		r.Host = host
		r.Header.Set("X-Panel-Token", token)
		r.Header.Set("Cookie", "panel_session=test-token")
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	for _, args := range [][4]string{{"/api/config", "attacker.test:8787", "test-token", ""}, {"/api/config", "localhost:8787", "", ""}, {"/api/config", "localhost:8787", "test-token", "https://attacker.test"}} {
		if request(args[0], args[1], args[2], args[3]).Code != 403 {
			t.Fatal("unauthorized access allowed")
		}
	}
	w := request("/api/config", "localhost:8787", "test-token", "")
	if w.Code != 200 || strings.Contains(w.Body.String(), "secret-path") || strings.Contains(w.Body.String(), "telegram_chat") {
		t.Fatal(w.Body.String())
	}
	w = request("/api/status", "localhost:8787", "test-token", "")
	if strings.Contains(w.Body.String(), "private answer") || strings.Contains(w.Body.String(), "secret reply") {
		t.Fatal("private history exposed")
	}
}

func TestPanelRetryDoesNotDuplicateWorkers(t *testing.T) {
	c := fixture(t)
	j := &Job{ID: "task", Stage: "running", Role: "executor"}
	c.State.Jobs[j.ID] = j
	if c.executePanelCommand(PanelCommand{Action: "retry", ID: j.ID}) == nil || j.Stage != "running" {
		t.Fatal("live worker retried")
	}
	j.Stage = "waiting"
	j.QuestionOrigin = "agent"
	if c.executePanelCommand(PanelCommand{Action: "retry", ID: j.ID}) == nil {
		t.Fatal("question bypassed")
	}
	j.Question = "Which mode?"
	if err := c.executePanelCommand(PanelCommand{Action: "answer", ID: j.ID, Text: "Existing mode"}); err != nil {
		t.Fatal(err)
	}
	if j.Stage != "queued" || len(j.Clarifications) != 1 {
		t.Fatal("answer not retained")
	}
	if _, err := c.validatedPanelConfig(map[string]json.RawMessage{"native_root": json.RawMessage(`"unsafe"`)}); err == nil {
		t.Fatal("isolation reconfigured")
	}
}
