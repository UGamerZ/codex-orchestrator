package system

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestManualBoardCheck(t *testing.T) {
	for _, tc := range []struct {
		name string
		chat int64
		busy bool
	}{{"authorized", 123, false}, {"other chat", 999, false}, {"busy manager", 123, true}} {
		t.Run(tc.name, func(t *testing.T) {
			c := fixture(t)
			c.C.Todo = "todo"
			c.C.Board = "board"
			c.State.LastManager = time.Now().UTC()
			scheduled := c.State.LastManager
			if tc.busy {
				c.State.Jobs["manager"] = &Job{ID: "manager", Role: "manager", Stage: "running"}
				c.State.Pending = []*Job{{ID: "original"}}
			}
			cardCalls := 0
			updates := 0
			c.API.ghTransport = func(context.Context, ...string) ([]byte, error) { return []byte(`[[]]`), nil }
			c.API.Client.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
				switch {
				case strings.HasSuffix(r.URL.Path, "/getUpdates"):
					updates++
					if updates > 1 {
						return response(`{"ok":true,"result":[]}`), nil
					}
					text := `{"ok":true,"result":[{"update_id":1,"message":{"text":"/check","chat":{"id":CHAT,"type":"private"}}}]}`
					chat := "123"
					if tc.chat == 999 {
						chat = "999"
					}
					return response(strings.Replace(text, "CHAT", chat, 1)), nil
				case strings.HasSuffix(r.URL.Path, "/sendMessage"):
					return response(`{"ok":true,"result":{"message_id":42}}`), nil
				case strings.HasSuffix(r.URL.Path, "/cards"):
					cardCalls++
					return response(`[{"id":"new-card","name":"New task","idList":"todo","closed":false}]`), nil
				default:
					t.Fatalf("unexpected request: %s", r.URL.Path)
					return nil, nil
				}
			})
			if e := c.poll(context.Background()); e != nil {
				t.Fatal(e)
			}
			if !c.State.LastManager.Equal(scheduled) {
				t.Fatal("manual check changed hourly schedule")
			}
			if tc.chat != 123 {
				if cardCalls != 0 || c.State.BoardCheckRequested {
					t.Fatal("unauthorized board check")
				}
				return
			}
			if tc.busy {
				if cardCalls != 0 || !c.State.BoardCheckRequested || c.State.Pending[0].ID != "original" {
					t.Fatal("active manager snapshot overwritten")
				}
				c.State.Jobs["manager"].Stage = "done"
				if e := c.poll(context.Background()); e != nil {
					t.Fatal(e)
				}
			}
			if cardCalls != 1 || c.State.BoardCheckRequested || c.State.Jobs["manager"].Stage != "queued" || c.State.Pending[0].ID != "new-card" {
				t.Fatal("manual board check did not queue manager")
			}
		})
	}
}
