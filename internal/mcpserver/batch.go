package mcpserver

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"time"

	"github.com/drumandbytes/eraser/internal/broker"
	"github.com/drumandbytes/eraser/internal/config"
	"github.com/drumandbytes/eraser/internal/history"
	"github.com/drumandbytes/eraser/internal/template"
)

type batchPlan struct {
	ID            string
	Profile       config.NamedProfile
	Config        *config.Config
	ConfigDigest  string
	Tasks         map[string]Task
	Brokers       map[string]broker.Broker
	Created       time.Time
	LastSend      time.Time
	PreviewDigest string
}

func profileFields(p config.Profile) map[string]string {
	return map[string]string{"first_name": p.FirstName, "last_name": p.LastName, "full_name": p.FullName(), "email": p.Email, "phone": p.Phone, "address": p.Address, "city": p.City, "state": p.State, "zip_code": p.ZipCode, "country": p.Country, "date_of_birth": p.DateOfBirth}
}

func (s *Service) prepare(_ context.Context, in prepareInput) (map[string]any, error) {
	if len(in.TaskIDs)+len(in.BrokerIDs) == 0 || len(in.TaskIDs)+len(in.BrokerIDs) > 100 {
		return nil, fmt.Errorf("select between 1 and 100 explicit tasks/brokers")
	}
	cfg, p, db, err := s.load()
	if err != nil {
		return nil, err
	}
	plan := &batchPlan{ID: rand.Text(), Profile: p, Config: cfg, ConfigDigest: digest(struct {
		C *config.Config
		B *broker.BrokerDatabase
	}{cfg, db}), Tasks: map[string]Task{}, Brokers: map[string]broker.Broker{}, Created: time.Now()}
	tasks := []Task{}
	for _, id := range in.TaskIDs {
		t, err := s.resolveTask(p, db, id)
		if err != nil {
			return nil, err
		}
		if !eligible(cfg, db, t.BrokerID) {
			return nil, fmt.Errorf("broker %s is excluded by current settings", t.BrokerID)
		}
		if t.Status != "pending" || t.Kind == "review" || t.URL == "" {
			return nil, fmt.Errorf("task %s needs review in Eraser before automation", id)
		}
		if _, ok := plan.Tasks[id]; ok {
			continue
		}
		plan.Tasks[id] = t
		tasks = append(tasks, t)
	}
	engine, err := template.NewEngine()
	if err != nil {
		return nil, err
	}
	drafts := []map[string]any{}
	for _, id := range in.BrokerIDs {
		b := db.FindByID(id)
		if b == nil || !eligible(cfg, db, id) {
			return nil, fmt.Errorf("unknown or excluded broker %s", id)
		}
		if b.Email == "" {
			return nil, fmt.Errorf("broker %s requires its web form", id)
		}
		if _, ok := plan.Brokers[id]; ok {
			continue
		}
		mail, err := engine.Render(cfg.Options.Template, p.Profile, *b)
		if err != nil {
			return nil, err
		}
		plan.Brokers[id] = *b
		drafts = append(drafts, map[string]any{"broker_id": id, "to": b.Email, "from": cfg.EmailForProfile(p).From, "subject": mail.Subject, "body": mail.Body})
	}
	// Bound pending previews and expire unused ones without retaining personal
	// information indefinitely in a long-lived process.
	for id, old := range s.plans {
		if time.Since(old.Created) > 15*time.Minute {
			delete(s.plans, id)
		}
	}
	if len(s.plans) >= 20 {
		return nil, fmt.Errorf("too many unused previews; end the batch or restart the MCP server")
	}
	out := map[string]any{"plan_id": plan.ID, "profile_id": p.ID, "profile_fields": profileFields(p.Profile), "tasks": tasks, "emails": drafts, "expires_in_minutes": 15, "requires_user_approval": true, "approve_with": "Approve in Eraser's web UI (Automation page) or run: eraser mcp approve " + plan.ID}
	plan.PreviewDigest = digest(out)
	if err := s.store.CreateBatchApproval(history.BatchApproval{PlanID: plan.ID, ProfileID: p.ID, Digest: plan.PreviewDigest, Preview: digestPreview(out), ExpiresAt: plan.Created.Add(15 * time.Minute)}); err != nil {
		return nil, fmt.Errorf("store batch preview: %w", err)
	}
	s.plans[plan.ID] = plan
	return out, nil
}

func (s *Service) begin(_ context.Context, in beginInput) (map[string]any, error) {
	if s.active != nil {
		return nil, fmt.Errorf("end the current batch first")
	}
	p := s.plans[in.PlanID]
	if p == nil || time.Since(p.Created) > 15*time.Minute {
		return nil, fmt.Errorf("preview expired; prepare a new batch")
	}
	if err := s.checkConfig(p); err != nil {
		return nil, err
	}
	if err := s.store.ConsumeBatchApproval(p.ID, p.PreviewDigest); err != nil {
		return nil, fmt.Errorf("batch is not approved: approve it in Eraser's web UI or run eraser mcp approve %s", p.ID)
	}
	s.active = p
	delete(s.plans, in.PlanID)
	return map[string]any{"batch_id": p.ID, "profile_id": p.Profile.ID, "task_count": len(p.Tasks), "email_count": len(p.Brokers)}, nil
}

func (s *Service) checkConfig(p *batchPlan) error {
	cfg, _, db, err := s.load()
	if err != nil {
		return err
	}
	if digest(struct {
		C *config.Config
		B *broker.BrokerDatabase
	}{cfg, db}) != p.ConfigDigest {
		return fmt.Errorf("settings or broker destinations changed; end this batch and review a new preview")
	}
	return nil
}

func (s *Service) authorize(id string) (*batchPlan, error) {
	if s.active == nil || id == "" || id != s.active.ID {
		return nil, fmt.Errorf("no matching approved batch; prepare_batch and begin_batch first")
	}
	if err := s.checkConfig(s.active); err != nil {
		return nil, err
	}
	return s.active, nil
}

func (s *Service) endBatch() {
	for _, p := range s.pages {
		p.browser.Close()
	}
	s.pages = map[string]*taskPage{}
	s.active = nil
	s.plans = map[string]*batchPlan{}
}

// digestPreview is the stored copy of the preview the approver reviews.
func digestPreview(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}
