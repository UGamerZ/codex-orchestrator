package system

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

type API struct {
	C           Config
	Client      *http.Client
	ghTransport func(context.Context, ...string) ([]byte, error)
}

func NewAPI(c Config) *API { return &API{C: c, Client: &http.Client{Timeout: 35 * time.Second}} }
func accountToken(ctx context.Context, c Config) ([]byte, error) {
	cmd := exec.CommandContext(ctx, c.GHPath, "auth", "token", "--hostname", "github.com", "--user", c.GithubUser)
	cmd.Env = withoutEnv(os.Environ(), "GH_TOKEN", "GITHUB_TOKEN", "GH_HOST")
	return cmd.Output()
}
func (a *API) gh(ctx context.Context, args ...string) ([]byte, error) {
	if a.ghTransport != nil {
		return a.ghTransport(ctx, args...)
	}
	t, e := accountToken(ctx, a.C)
	if e != nil {
		return nil, fmt.Errorf("GitHub account %s authentication unavailable", a.C.GithubUser)
	}
	cmd := exec.CommandContext(ctx, a.C.GHPath, args...)
	cmd.Env = withoutEnv(os.Environ(), "GH_TOKEN", "GITHUB_TOKEN", "GH_HOST")
	cmd.Env = append(cmd.Env, "GH_TOKEN="+strings.TrimSpace(string(t)), "GH_HOST=github.com")
	b, e := cmd.Output()
	if e != nil {
		return nil, fmt.Errorf("GitHub operation failed (check account access, connectivity or limits)")
	}
	return b, nil
}
func withoutEnv(env []string, keys ...string) []string {
	out := []string{}
	for _, v := range env {
		keep := true
		for _, k := range keys {
			if strings.HasPrefix(strings.ToUpper(v), strings.ToUpper(k)+"=") {
				keep = false
			}
		}
		if keep {
			out = append(out, v)
		}
	}
	return out
}

type Pull struct {
	Number int    `json:"number"`
	URL    string `json:"html_url"`
	Title  string `json:"title"`
	Body   string `json:"body"`
	Head   struct {
		SHA  string `json:"sha"`
		Ref  string `json:"ref"`
		Repo struct {
			FullName string `json:"full_name"`
		} `json:"repo"`
	} `json:"head"`
	Base struct {
		SHA string `json:"sha"`
		Ref string `json:"ref"`
	} `json:"base"`
}

func (a *API) Pulls(ctx context.Context) ([]Pull, error) {
	if a.C.ProjectMode == "local" {
		return a.localReviews(ctx)
	}
	p := "repos/" + a.C.Repository + "/pulls?state=open&base=" + url.QueryEscape(a.C.BaseBranch) + "&per_page=100"
	b, e := a.gh(ctx, "api", "--paginate", "--slurp", p)
	if e != nil {
		return nil, e
	}
	var pages [][]Pull
	if e = json.Unmarshal(b, &pages); e != nil {
		return nil, e
	}
	out := []Pull{}
	for _, page := range pages {
		out = append(out, page...)
	}
	return out, nil
}

// BranchHead confirms a PR-list discrepancy against the branch itself.
// GitHub's PR list can briefly lag behind a successful push.
func (a *API) BranchHead(ctx context.Context, p Pull) (string, error) {
	if a.C.ProjectMode == "local" {
		return p.Head.SHA, nil
	}
	if p.Head.Repo.FullName == "" || p.Head.Ref == "" {
		return "", fmt.Errorf("PR head repository or branch unavailable")
	}
	b, e := a.gh(ctx, "api", "repos/"+p.Head.Repo.FullName+"/git/ref/heads/"+url.PathEscape(p.Head.Ref), "-H", "Cache-Control: no-cache")
	if e != nil {
		return "", e
	}
	var ref struct {
		Object struct {
			SHA string `json:"sha"`
		} `json:"object"`
	}
	if e = json.Unmarshal(b, &ref); e != nil {
		return "", e
	}
	if ref.Object.SHA == "" {
		return "", fmt.Errorf("branch head unavailable")
	}
	return ref.Object.SHA, nil
}
func (a *API) CreatePR(ctx context.Context, j *Job) (int, string, error) {
	if a.C.ProjectMode == "local" {
		return a.createLocalReview(ctx, j)
	}
	// Recover a PR created immediately before a crash; never create duplicates.
	b, e := a.gh(ctx, "api", "repos/"+a.C.Repository+"/pulls?state=all&head="+url.QueryEscape(strings.Split(a.C.Repository, "/")[0]+":"+j.Branch)+"&base="+url.QueryEscape(a.C.BaseBranch))
	if e != nil {
		return 0, "", e
	}
	var existing []Pull
	if e = json.Unmarshal(b, &existing); e != nil {
		return 0, "", e
	}
	if len(existing) > 0 {
		return existing[0].Number, existing[0].URL, nil
	}
	body := j.Summary + "\n\nTrello: https://trello.com/c/" + j.Card + "\n\nCreated by an isolated executor. Merging is performed by a human."
	b, e = a.gh(ctx, "api", "--method", "POST", "repos/"+a.C.Repository+"/pulls", "-f", "title="+j.Title, "-f", "head="+j.Branch, "-f", "base="+a.C.BaseBranch, "-f", "body="+body)
	if e != nil {
		return 0, "", e
	}
	var p Pull
	e = json.Unmarshal(b, &p)
	return p.Number, p.URL, e
}
func (a *API) Comment(ctx context.Context, pr int, body string) error {
	if a.C.ProjectMode == "local" {
		return a.localComment(pr, body)
	}
	// The sole reviewer publication capability is an issue comment.
	// No arbitrary endpoint or event can be supplied by an agent.
	_, e := a.gh(ctx, "api", "--method", "POST", fmt.Sprintf("repos/%s/issues/%d/comments", a.C.Repository, pr), "-f", "body="+body)
	return e
}

type Card struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Desc   string `json:"desc"`
	List   string `json:"idList"`
	Closed bool   `json:"closed"`
}

func (a *API) trello(ctx context.Context, method, path string, values url.Values, out any) error {
	if values == nil {
		values = url.Values{}
	}
	values.Set("key", os.Getenv("TRELLO_KEY"))
	values.Set("token", os.Getenv("TRELLO_TOKEN"))
	target := "https://api.trello.com/1/" + path
	var body io.Reader
	if method == "GET" {
		target += "?" + values.Encode()
	} else {
		body = strings.NewReader(values.Encode())
	}
	req, e := http.NewRequestWithContext(ctx, method, target, body)
	if e != nil {
		return e
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r, e := a.Client.Do(req)
	if e != nil {
		return fmt.Errorf("Trello network unavailable")
	}
	defer r.Body.Close()
	if r.StatusCode/100 != 2 {
		return fmt.Errorf("Trello HTTP %d", r.StatusCode)
	}
	if out != nil {
		return json.NewDecoder(io.LimitReader(r.Body, 4<<20)).Decode(out)
	}
	return nil
}
func (a *API) Cards(ctx context.Context) ([]Card, error) {
	var c []Card
	e := a.trello(ctx, "GET", "boards/"+a.C.Board+"/cards", url.Values{"fields": {"name,desc,idList,closed"}, "filter": {"all"}}, &c)
	return c, e
}
func (a *API) Move(ctx context.Context, card, list string) error {
	if list != a.C.Progress && list != a.C.Done {
		return fmt.Errorf("card destination forbidden")
	}
	if a.C.Board != "" {
		current, err := a.CardLocation(ctx, card)
		if err != nil {
			return err
		}
		if cardOutsideLists(a.C, current) {
			return errCardCompleted
		}
	}
	return a.trello(ctx, "PUT", "cards/"+card, url.Values{"idList": {list}}, nil)
}

type TGMessage struct {
	ID   int64  `json:"message_id"`
	Text string `json:"text"`
	Chat struct {
		ID   int64  `json:"id"`
		Type string `json:"type"`
	} `json:"chat"`
	Reply *TGMessage `json:"reply_to_message"`
}
type Update struct {
	ID      int64      `json:"update_id"`
	Message *TGMessage `json:"message"`
}

func (a *API) telegram(ctx context.Context, method string, payload any, out any) error {
	b, e := json.Marshal(payload)
	if e != nil {
		return e
	}
	req, e := http.NewRequestWithContext(ctx, "POST", "https://api.telegram.org/bot"+os.Getenv("TELEGRAM_TOKEN")+"/"+method, bytes.NewReader(b))
	if e != nil {
		return e
	}
	req.Header.Set("Content-Type", "application/json")
	r, e := a.Client.Do(req)
	if e != nil {
		return fmt.Errorf("Telegram network unavailable")
	}
	defer r.Body.Close()
	var envelope struct {
		OK     bool            `json:"ok"`
		Result json.RawMessage `json:"result"`
	}
	if e = json.NewDecoder(io.LimitReader(r.Body, 4<<20)).Decode(&envelope); e != nil {
		return e
	}
	if !envelope.OK {
		return fmt.Errorf("Telegram rejected operation (HTTP %d)", r.StatusCode)
	}
	if out != nil {
		return json.Unmarshal(envelope.Result, out)
	}
	return nil
}
func (a *API) Send(ctx context.Context, text string) (int64, error) {
	var m TGMessage
	e := a.telegram(ctx, "sendMessage", map[string]any{"chat_id": a.C.Chat, "text": clip(text, 3800)}, &m)
	return m.ID, e
}
func (a *API) Updates(ctx context.Context, offset int64) ([]Update, error) {
	var u []Update
	e := a.telegram(ctx, "getUpdates", map[string]any{"offset": offset, "timeout": 0, "allowed_updates": []string{"message"}}, &u)
	return u, e
}
func clip(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}
func (a *API) Validate(ctx context.Context) error {
	if e := a.ValidateProject(ctx); e != nil {
		return e
	}
	var e error
	var lists []struct {
		ID string `json:"id"`
	}
	if e = a.trello(ctx, "GET", "boards/"+a.C.Board+"/lists", nil, &lists); e != nil {
		return e
	}
	found := map[string]bool{}
	for _, l := range lists {
		found[l.ID] = true
	}
	for _, id := range []string{a.C.Todo, a.C.Progress, a.C.Done} {
		if !found[id] {
			return fmt.Errorf("list %s is not on configured board", id)
		}
	}
	var me struct {
		ID int64 `json:"id"`
	}
	if e = a.telegram(ctx, "getMe", map[string]any{}, &me); e != nil {
		return e
	}
	var hook struct {
		URL string `json:"url"`
	}
	if e = a.telegram(ctx, "getWebhookInfo", map[string]any{}, &hook); e != nil {
		return e
	}
	if hook.URL != "" {
		return fmt.Errorf("Telegram bot has an active webhook; use a dedicated polling bot")
	}
	var chat struct {
		Type string `json:"type"`
	}
	if e = a.telegram(ctx, "getChat", map[string]any{"chat_id": a.C.Chat}, &chat); e != nil {
		return e
	}
	if chat.Type != "private" {
		return fmt.Errorf("only private Telegram chat allowed: %s", strconv.FormatInt(a.C.Chat, 10))
	}
	return nil
}

func (a *API) CardLocation(ctx context.Context, id string) (Card, error) {
	var card Card
	err := a.trello(ctx, "GET", "cards/"+id, url.Values{"fields": {"id,idList,closed"}}, &card)
	if err == nil && (card.ID != id || (!card.Closed && card.List == "")) {
		return card, fmt.Errorf("invalid card location response")
	}
	return card, err
}
