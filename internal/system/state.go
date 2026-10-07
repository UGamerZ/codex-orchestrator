package system

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type Job struct {
	InvalidResultRetries int             `json:"invalid_result_retries,omitempty"`
	ModelBusyRetries     int             `json:"model_busy_retries,omitempty"`
	ReviewPolicyVersion  int             `json:"review_policy_version,omitempty"`
	CommitTitle          string          `json:"commit_title,omitempty"`
	Clarifications       []Clarification `json:"clarifications,omitempty"`
	QuestionOrigin       string          `json:"question_origin,omitempty"`
	ClarificationRetries int             `json:"clarification_retries,omitempty"`
	LastError            string          `json:"last_error,omitempty"`
	ID                   string          `json:"id"`
	Card                 string          `json:"card,omitempty"`
	Title                string          `json:"title"`
	Description          string          `json:"description"`
	Branch               string          `json:"branch"`
	BaseSHA              string          `json:"base_sha"`
	Head                 string          `json:"head"`
	PR                   int             `json:"pr"`
	PRURL                string          `json:"pr_url"`
	External             bool            `json:"external"`
	Stage                string          `json:"stage"`
	Session              string          `json:"session"`
	ExecutorSession      string          `json:"executor_session"`
	ReviewerSession      string          `json:"reviewer_session"`
	Role                 string          `json:"role"`
	Question             string          `json:"question"`
	QuestionMessage      int64           `json:"question_message"`
	Answer               string          `json:"answer"`
	Summary              string          `json:"summary"`
	Findings             []string        `json:"findings"`
	Fixes                int             `json:"fixes"`
	RetryAt              time.Time       `json:"retry_at"`
	Fallback             bool            `json:"fallback"`
	NotifiedHead         string          `json:"notified_head"`
	ReviewedHead         string          `json:"reviewed_head"`
	DoneMoved            bool            `json:"done_moved"`
	ClosedPR             bool            `json:"closed_pr"`
}
type Clarification struct {
	Role     string    `json:"role"`
	Origin   string    `json:"origin,omitempty"`
	Question string    `json:"question"`
	Answer   string    `json:"answer"`
	At       time.Time `json:"at"`
}
type State struct {
	BoardCheckAfterDone bool            `json:"board_check_after_done,omitempty"`
	BoardCheckRequested bool            `json:"board_check_requested,omitempty"`
	Jobs                map[string]*Job `json:"jobs"`
	TelegramOffset      int64           `json:"telegram_offset"`
	LastManager         time.Time       `json:"last_manager"`
	Pending             []*Job          `json:"pending"`
}

func LoadState(dir string) (*State, error) {
	if e := os.MkdirAll(dir, 0700); e != nil {
		return nil, e
	}
	s := &State{Jobs: map[string]*Job{}}
	b, e := os.ReadFile(filepath.Join(dir, "state.json"))
	if os.IsNotExist(e) {
		return s, nil
	}
	if e != nil {
		return nil, e
	}
	if e = json.Unmarshal(b, s); e != nil {
		return nil, e
	}
	if s.Jobs == nil {
		return nil, fmt.Errorf("invalid saved jobs")
	}
	// Never rerun a potentially live container after a controller crash. Ask the owner.
	for _, j := range s.Jobs {
		if j.Stage == "running" || j.Stage == "publishing" {
			j.Stage = "waiting"
			j.Question = "The controller restarted during processing. The interrupted task is paused. Reply to retry and continue."
			j.QuestionMessage = 0
		}
	}
	return s, nil
}
func (s *State) Save(dir string) error {
	b, e := json.MarshalIndent(s, "", "  ")
	if e != nil {
		return e
	}
	p := filepath.Join(dir, "state.json")
	if e = os.WriteFile(p+".tmp", b, 0600); e != nil {
		return e
	}
	return os.Rename(p+".tmp", p)
}
