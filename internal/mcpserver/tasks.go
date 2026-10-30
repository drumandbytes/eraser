package mcpserver

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/drumandbytes/eraser/internal/broker"
	"github.com/drumandbytes/eraser/internal/config"
	"github.com/drumandbytes/eraser/internal/history"
)

type Task struct {
	ID         string `json:"id"`
	BrokerID   string `json:"broker_id"`
	BrokerName string `json:"broker_name"`
	Kind       string `json:"kind"`
	URL        string `json:"url"`
	Status     string `json:"status"`
	Notes      string `json:"notes,omitempty"`
	Screenshot string `json:"screenshot,omitempty"`
}

func taskFromPending(t history.PendingTask) Task {
	kind := "form"
	if t.TaskType == history.TaskConfirm {
		kind = "confirmation"
	}
	if t.TaskType == history.TaskReview {
		kind = "review"
	}
	return Task{ID: fmt.Sprintf("task:%d", t.ID), BrokerID: t.BrokerID, BrokerName: t.BrokerName, Kind: kind, URL: t.FormURL, Status: t.Status, Notes: t.Notes, Screenshot: t.ScreenshotPath}
}

func taskFromResponse(r history.BrokerResponse) Task {
	kind, raw := "form", r.FormURL
	if r.ConfirmURL != "" {
		kind, raw = "confirmation", r.ConfirmURL
	}
	if r.NeedsReview {
		kind = "review"
	}
	return Task{ID: fmt.Sprintf("response:%d", r.ID), BrokerID: r.BrokerID, BrokerName: r.BrokerName, Kind: kind, URL: raw, Status: "pending", Notes: r.EmailSubject}
}

func subject(t Task) string { return "task:" + t.BrokerID + ":" + digest(t.URL) }

func (s *Service) tasks(p config.NamedProfile, db *broker.BrokerDatabase) ([]Task, error) {
	all, err := s.store.GetPendingTasks(p.ID, "", "")
	if err != nil {
		return nil, err
	}
	out := []Task{}
	seen := map[string]bool{}
	for _, p := range all {
		t := taskFromPending(p)
		seen[subject(t)] = true
		if t.Status == "pending" {
			out = append(out, t)
		}
	}
	responses, err := s.store.GetBrokerResponses(p.ID, "", false, 10000)
	if err != nil {
		return nil, err
	}
	for _, r := range responses {
		t := taskFromResponse(r)
		if t.URL == "" || seen[subject(t)] {
			continue
		}
		seen[subject(t)] = true
		out = append(out, t)
	}
	for _, b := range db.Brokers {
		if b.Email != "" || b.OptOutURL == "" || !b.AppliesTo(p.Country) {
			continue
		}
		t := Task{ID: "broker:" + b.ID, BrokerID: b.ID, BrokerName: b.Name, Kind: "form", URL: b.OptOutURL, Status: "pending", Notes: b.Notes}
		if !seen[subject(t)] {
			out = append(out, t)
		}
	}
	return out, nil
}

func eligible(cfg *config.Config, db *broker.BrokerDatabase, id, country string) bool {
	for _, b := range db.Filter(cfg.Options.Regions, cfg.Options.ExcludedBrokers, cfg.Options.ExcludedCategories) {
		if b.ID == id && b.AppliesTo(country) {
			return true
		}
	}
	return false
}

func (s *Service) listTasks(_ context.Context, in listInput) (map[string]any, error) {
	cfg, p, db, err := s.load()
	if err != nil {
		return nil, err
	}
	all, err := s.tasks(p, db)
	if err != nil {
		return nil, err
	}
	filtered := []Task{}
	for _, t := range all {
		if eligible(cfg, db, t.BrokerID, p.Country) {
			filtered = append(filtered, t)
		}
	}
	a, b := window(in, len(filtered))
	return map[string]any{"tasks": filtered[a:b], "total": len(filtered), "next_offset": b}, nil
}

func (s *Service) resolveTask(p config.NamedProfile, db *broker.BrokerDatabase, id string) (Task, error) {
	kind, value, ok := strings.Cut(id, ":")
	if !ok {
		return Task{}, fmt.Errorf("use a task ID from list_tasks")
	}
	if kind == "broker" {
		b := db.FindByID(value)
		if b == nil || b.OptOutURL == "" || !b.AppliesTo(p.Country) {
			return Task{}, fmt.Errorf("broker form not found")
		}
		return Task{ID: id, BrokerID: b.ID, BrokerName: b.Name, Kind: "form", URL: b.OptOutURL, Status: "pending", Notes: b.Notes}, nil
	}
	n, err := strconv.ParseInt(value, 10, 64)
	if err != nil || n <= 0 {
		return Task{}, fmt.Errorf("invalid task ID")
	}
	switch kind {
	case "task":
		t, err := s.store.GetPendingTaskByID(n, p.ID)
		if err != nil {
			return Task{}, err
		}
		if t != nil {
			return taskFromPending(*t), nil
		}
	case "response":
		r, err := s.store.GetBrokerResponseByID(n, p.ID)
		if err != nil {
			return Task{}, err
		}
		if r != nil {
			return taskFromResponse(*r), nil
		}
	}
	return Task{}, fmt.Errorf("task not found for the selected profile")
}

func (s *Service) getTask(_ context.Context, in taskInput) (map[string]any, error) {
	_, p, db, err := s.load()
	if err != nil {
		return nil, err
	}
	t, err := s.resolveTask(p, db, in.TaskID)
	if err != nil {
		return nil, err
	}
	attempts, err := s.store.GetAutomationAttempts(p.ID, subject(t))
	if err != nil {
		return nil, err
	}
	out := map[string]any{"task": t, "attempts": attempts}
	if raw, ok := strings.CutPrefix(t.ID, "response:"); ok {
		id, _ := strconv.ParseInt(raw, 10, 64)
		r, err := s.store.GetBrokerResponseByID(id, p.ID)
		if err != nil {
			return nil, err
		}
		if r != nil {
			body := []rune(r.EmailBody)
			out["reply"] = map[string]any{"from": r.EmailFrom, "subject": r.EmailSubject, "body": string(body[:min(len(body), 16000)]), "truncated": len(body) > 16000, "untrusted": true}
		}
	}
	return out, nil
}
