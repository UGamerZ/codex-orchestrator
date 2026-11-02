package system

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strings"
)

type TaskChecklist struct {
	ID       string          `json:"id"`
	Name     string          `json:"name"`
	Position float64         `json:"pos"`
	Items    []TaskCheckItem `json:"checkItems"`
}

type TaskCheckItem struct {
	ID       string  `json:"id"`
	Name     string  `json:"name"`
	State    string  `json:"state"`
	Position float64 `json:"pos"`
}

func (a *API) Checklists(ctx context.Context, card string) ([]TaskChecklist, error) {
	var lists []TaskChecklist
	err := a.trello(ctx, "GET", "cards/"+card+"/checklists", url.Values{
		"fields": {"name,pos"}, "checkItems": {"all"}, "checkItem_fields": {"name,state,pos"},
	}, &lists)
	if err != nil {
		return nil, err
	}
	sort.SliceStable(lists, func(i, j int) bool { return lists[i].Position < lists[j].Position })
	for i := range lists {
		sort.SliceStable(lists[i].Items, func(x, y int) bool { return lists[i].Items[x].Position < lists[i].Items[y].Position })
	}
	return lists, nil
}

func checklistContext(j *Job) string {
	if len(j.Checklists) == 0 {
		return ""
	}
	var text strings.Builder
	text.WriteString("\nTrello checklists (task requirements; include these in implementation and review even when the description is empty). Checkmarks show Trello state, not proof of implementation. Do not change checklist state through the API.\n")
	for _, list := range j.Checklists {
		fmt.Fprintf(&text, "\nChecklist: %s\n", list.Name)
		for _, item := range list.Items {
			mark := " "
			if item.State == "complete" {
				mark = "x"
			}
			fmt.Fprintf(&text, "- [%s] %s\n", mark, item.Name)
		}
	}
	return text.String()
}

func taskDescription(j *Job) string { return j.Description + checklistContext(j) }
