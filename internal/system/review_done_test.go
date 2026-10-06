package system

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestDoneRequiresReviewOfCurrentHead(t *testing.T) {
	for _, tc := range []struct {
		name, stage, reviewed string
		wasDone               bool
		want                  string
	}{{"PR created", "review", "", false, ""}, {"review blocked", "waiting", "", true, "progress"}, {"stale approval", "passed", "old", false, ""}, {"current approval", "passed", "head", false, "done"}} {
		t.Run(tc.name, func(t *testing.T) {
			c := fixture(t)
			c.C.Done = "done"
			c.C.Progress = "progress"
			c.API.C = c.C
			c.State.LastManager = time.Now()
			j := &Job{ID: "task", Card: "card", PR: 7, Head: "head", ReviewedHead: tc.reviewed, Stage: tc.stage, Role: "reviewer", DoneMoved: tc.wasDone, QuestionMessage: 99, NotifiedHead: "head"}
			c.State.Jobs[j.ID] = j
			fakePull(c.API, "head")
			moved := ""
			c.API.Client.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
				if strings.HasSuffix(r.URL.Path, "/getUpdates") {
					return response(`{"ok":true,"result":[]}`), nil
				}
				if r.Method == "PUT" && strings.HasSuffix(r.URL.Path, "/cards/card") {
					r.ParseForm()
					moved = r.Form.Get("idList")
					return response(`{}`), nil
				}
				t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
				return nil, nil
			})
			if e := c.poll(context.Background()); e != nil {
				t.Fatal(e)
			}
			if moved != tc.want {
				t.Fatalf("card moved to %q; want %q", moved, tc.want)
			}
		})
	}
}
