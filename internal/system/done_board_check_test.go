package system

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestDoneTriggersBoardCheck(t *testing.T) {
	for _, tc := range []struct {
		name                       string
		busy, moveFails, scanFails bool
	}{
		{name: "immediate"}, {name: "busy manager", busy: true}, {name: "failed Done move", moveFails: true}, {name: "failed scan retries", scanFails: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := fixture(t)
			c.C.Done = "done"
			c.C.Todo = "todo"
			c.API.C = c.C
			last := time.Now()
			c.State.LastManager = last
			c.State.Jobs["task"] = &Job{ID: "task", Card: "card", PR: 7, Head: "head", ReviewedHead: "head", Stage: "passed", NotifiedHead: "head"}
			if tc.busy {
				c.State.Jobs["manager"] = &Job{ID: "manager", Role: "manager", Stage: "running"}
			}
			fakePull(c.API, "head")
			scans, moves, notifications := 0, 0, 0
			c.API.Client.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
				switch {
				case strings.HasSuffix(r.URL.Path, "/sendMessage"):
					notifications++
					return response(`{"ok":true,"result":{"message_id":50}}`), nil
				case strings.HasSuffix(r.URL.Path, "/getUpdates"):
					return response(`{"ok":true,"result":[]}`), nil
				case r.Method == "PUT" && strings.HasSuffix(r.URL.Path, "/cards/card"):
					moves++
					resp := response(`{}`)
					if tc.moveFails {
						resp.StatusCode = 500
					}
					return resp, nil
				case r.Method == "GET" && strings.HasSuffix(r.URL.Path, "/cards"):
					scans++
					resp := response(`[{"id":"next","name":"Next task","idList":"todo"}]`)
					if tc.scanFails && scans == 1 {
						resp.StatusCode = 500
					}
					return resp, nil
				default:
					t.Fatalf("unexpected request %s", r.URL.Path)
					return nil, nil
				}
			})
			err := c.poll(context.Background())
			if tc.scanFails && err == nil {
				t.Fatal("scan failure hidden")
			}
			if !tc.scanFails && err != nil {
				t.Fatal(err)
			}
			if tc.moveFails {
				if notifications != 1 {
					t.Fatal("failed card move was not reported to Telegram")
				}
				if scans != 0 || c.State.BoardCheckAfterDone {
					t.Fatal("failed move triggered check")
				}
				return
			}
			if tc.busy || tc.scanFails {
				if !c.State.BoardCheckAfterDone {
					t.Fatal("check request lost")
				}
				if tc.busy && scans != 0 {
					t.Fatal("busy manager's pending tasks replaced")
				}
				if e := c.save(); e != nil {
					t.Fatal(e)
				}
				state, e := LoadState(c.C.StateDir)
				if e != nil || !state.BoardCheckAfterDone {
					t.Fatal("check not persisted")
				}
				if tc.busy {
					c.State.Jobs["manager"].Stage = "done"
				}
				if e := c.poll(context.Background()); e != nil {
					t.Fatal(e)
				}
			}
			if moves != 1 || c.State.BoardCheckAfterDone || c.State.Jobs["manager"].Stage != "queued" || len(c.State.Pending) != 1 || c.State.Pending[0].ID != "next" {
				t.Fatal("Done did not queue manager correctly")
			}
			if !c.State.LastManager.Equal(last) {
				t.Fatal("Done changed hourly schedule")
			}
			before := scans
			if e := c.poll(context.Background()); e != nil {
				t.Fatal(e)
			}
			if scans != before {
				t.Fatal("existing Done card triggered repeated check")
			}
		})
	}
}
