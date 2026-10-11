package system

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func panelHandler(configPath, stateDir, token string, commands chan PanelCommand, logins ...*GitHubLoginSession) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, _, err := net.SplitHostPort(r.Host)
		if err != nil || (host != "127.0.0.1" && host != "localhost" && host != "::1") {
			http.Error(w, "Local access only", 403)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'nonce-"+token+"'; style-src 'nonce-"+token+"'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
		if r.URL.Path == "/" && r.Method == "GET" {
			http.SetCookie(w, &http.Cookie{Name: "panel_session", Value: token, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode})
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			fmt.Fprint(w, strings.ReplaceAll(panelHTML, "PANEL_TOKEN", token))
			return
		}
		cookie, e := r.Cookie("panel_session")
		if e != nil || subtle.ConstantTimeCompare([]byte(cookie.Value), []byte(token)) != 1 || subtle.ConstantTimeCompare([]byte(r.Header.Get("X-Panel-Token")), []byte(token)) != 1 {
			http.Error(w, "Session required", 403)
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" && origin != "http://"+r.Host {
			http.Error(w, "Origin rejected", 403)
			return
		}
		activeStateDir := stateDir
		if b, err := os.ReadFile(configPath); err == nil {
			var active Config
			if json.Unmarshal(b, &active) == nil && active.StateDir != "" {
				activeStateDir = active.StateDir
			}
		}
		switch {
		case r.URL.Path == "/api/environments" && r.Method == "GET":
			profiles, err := publicEnvironmentProfiles(configPath)
			if err != nil {
				http.Error(w, "Environment registry unavailable", 503)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(profiles)
		case r.URL.Path == "/api/github-accounts" && r.Method == "GET":
			b, _ := os.ReadFile(configPath)
			var active Config
			json.Unmarshal(b, &active)
			accounts, err := githubAccounts(active.GHPath)
			if err != nil {
				http.Error(w, err.Error(), 503)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(accounts)
		case r.URL.Path == "/api/github-login" && r.Method == "GET":
			status := GitHubLoginStatus{Status: "unavailable"}
			if len(logins) > 0 && logins[0] != nil {
				status = logins[0].Snapshot()
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(status)
		case r.URL.Path == "/api/status" && r.Method == "GET":
			data, e := os.ReadFile(filepath.Join(activeStateDir, "state.json"))
			if e != nil {
				http.Error(w, "State unavailable", 503)
				return
			}
			var state State
			if json.Unmarshal(data, &state) != nil {
				http.Error(w, "State unavailable", 503)
				return
			}
			// Do not expose credentials, raw conversation history, or integration secrets.
			for _, j := range state.Jobs {
				j.Clarifications = nil
				j.Answer = ""
				j.Description = ""
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(state)
		case r.URL.Path == "/api/agent-logs" && r.Method == "GET":
			logs, err := readAgentLogs(configPath, activeStateDir, r.URL.Query().Get("id"), r.URL.Query().Get("role"))
			if err != nil {
				http.Error(w, "Log unavailable: "+err.Error(), 400)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(logs)
		case r.URL.Path == "/api/activity" && r.Method == "GET":
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(nativePanelActivity(configPath, activeStateDir))
		case r.URL.Path == "/api/config" && r.Method == "GET":
			data, e := os.ReadFile(configPath)
			if e != nil {
				http.Error(w, "Configuration unavailable", 503)
				return
			}
			var all map[string]json.RawMessage
			json.Unmarshal(data, &all)
			safe := map[string]json.RawMessage{}
			for k, v := range all {
				if panelFields[k] {
					safe[k] = v
				}
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(safe)
		case r.URL.Path == "/api/command" && r.Method == "POST":
			var command PanelCommand
			decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16384))
			decoder.DisallowUnknownFields()
			if decoder.Decode(&command) != nil {
				http.Error(w, "Invalid command", 400)
				return
			}
			command.Result = make(chan error, 1)
			select {
			case commands <- command:
			case <-r.Context().Done():
				return
			default:
				http.Error(w, "Command queue full", 503)
				return
			}
			select {
			case err := <-command.Result:
				if err != nil {
					http.Error(w, err.Error(), 400)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprint(w, `{"ok":true}`)
			case <-r.Context().Done():
				return
			case <-time.After(60 * time.Second):
				http.Error(w, "Command queued; check event history before submitting again", 202)
			}
		default:
			http.NotFound(w, r)
		}
	})
}

func (c *Controller) StartPanel(ctx context.Context, address, configPath string) error {
	absolute, err := filepath.Abs(configPath)
	if err != nil {
		return err
	}
	configPath = absolute
	c.ConfigPath = configPath
	if err := c.initializeEnvironments(); err != nil {
		return err
	}
	c.Login = &GitHubLoginSession{path: c.C.GHPath, status: GitHubLoginStatus{Status: "idle"}}
	c.Commands = make(chan PanelCommand, 32)
	if address == "" {
		return nil
	}
	host, _, err := net.SplitHostPort(address)
	if err != nil || net.ParseIP(host) == nil || !net.ParseIP(host).IsLoopback() {
		return fmt.Errorf("dashboard must bind an explicit loopback address")
	}
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return err
	}
	nonce := make([]byte, 32)
	if _, err = rand.Read(nonce); err != nil {
		listener.Close()
		return err
	}
	server := &http.Server{Handler: panelHandler(configPath, c.C.StateDir, hex.EncodeToString(nonce), c.Commands, c.Login), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 75 * time.Second, IdleTimeout: 60 * time.Second}
	go func() {
		if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
			fmt.Fprintln(os.Stderr, "Dashboard server stopped:", err)
		}
	}()
	go func() {
		<-ctx.Done()
		stop, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if c.Login != nil {
			c.Login.Cancel()
		}
		server.Shutdown(stop)
	}()
	return nil
}
