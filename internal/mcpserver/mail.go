package mcpserver

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"github.com/drumandbytes/eraser/internal/email"
	"github.com/drumandbytes/eraser/internal/history"
	"github.com/drumandbytes/eraser/internal/inbox"
	"github.com/drumandbytes/eraser/internal/schedule"
	"github.com/drumandbytes/eraser/internal/template"
)

func (s *Service) scan(ctx context.Context, _ emptyInput) (map[string]any, error) {
	cfg, p, db, err := s.load()
	if err != nil {
		return nil, err
	}
	if s.demo != nil {
		return map[string]any{"demo": true, "new_replies": 0, "note": "Demo tasks are preloaded; no real mailbox is accessed."}, nil
	}
	ic := cfg.InboxForProfile(p)
	if !ic.Enabled || ic.Server == "" {
		return nil, fmt.Errorf("configure Inbox Monitoring in Eraser's existing Settings page")
	}
	ic.AutoArchive = false
	monitor := inbox.NewMonitor(ic, db.Brokers)
	if err := monitor.Connect(ctx); err != nil {
		return nil, fmt.Errorf("inbox connection failed; check Eraser's mail settings")
	}
	defer func() { _ = monitor.Disconnect() }()
	emails, err := monitor.FetchBrokerEmails(ctx, 30)
	if err != nil {
		return nil, fmt.Errorf("could not fetch broker replies")
	}
	added := 0
	for i := range emails {
		owner, err := s.store.ResolveProfileForBroker(emails[i].BrokerID)
		if err != nil {
			return nil, err
		}
		if owner != p.ID {
			continue
		}
		_, outcome, err := inbox.RecordReplyForProfile(s.store, &emails[i], p.ID, false)
		if err != nil {
			return nil, err
		}
		if outcome == inbox.ReplyNew {
			added++
		}
	}
	return map[string]any{"new_replies": added, "profile_id": p.ID}, nil
}

func (s *Service) send(ctx context.Context, in sendInput) (map[string]any, error) {
	p, err := s.authorize(in.BatchID)
	if err != nil {
		return nil, err
	}
	b, ok := p.Brokers[in.BrokerID]
	if !ok {
		return nil, fmt.Errorf("broker is outside the approved email batch")
	}
	if s.demo != nil {
		return map[string]any{"demo": true, "sent": false, "note": "The demo does not send email."}, nil
	}
	if p.Config.IsManualSend() {
		return nil, fmt.Errorf("manual send mode is enabled; use Eraser's existing draft/email UI")
	}
	if p.Config.Options.DryRun {
		return map[string]any{"dry_run": true, "sent": false}, nil
	}
	// same lock as scheduled cycles and other MCP senders
	release, ok, err := schedule.TryLock(filepath.Dir(s.opts.ConfigPath))
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("another send cycle is running")
	}
	defer release()
	last, err := s.store.LastSuccessfulSendTimes(p.Profile.ID)
	if err != nil {
		return nil, err
	}
	if !last[b.ID].IsZero() && time.Since(last[b.ID]) < history.ResendCooldown {
		return map[string]any{"sent": false, "skipped": "cooldown"}, nil
	}
	count, err := s.store.CountSentSince(p.Profile.ID, time.Now().Add(-24*time.Hour))
	if err != nil {
		return nil, err
	}
	if count >= p.Config.Options.DailySendLimit {
		return map[string]any{"sent": false, "skipped": "daily limit"}, nil
	}
	if wait := time.Until(p.LastSend.Add(time.Duration(p.Config.Options.RateLimitMs) * time.Millisecond)); wait > 0 {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(wait):
		}
	}
	engine, err := template.NewEngine()
	if err != nil {
		return nil, err
	}
	if err := engine.UseConfig(p.Config); err != nil {
		return nil, err
	}
	ec := p.Config.EmailForProfile(p.Profile)
	if !ec.Configured() {
		return nil, fmt.Errorf("configure email in Eraser's Settings")
	}
	sender := email.NewSMTPSender(ec.SMTP, ec.From)
	// count bounces too: a new send must not collide with an accepted message later marked bounced
	status, err := s.store.GetBrokerStatus(p.Profile.ID, b.ID)
	if err != nil {
		return nil, err
	}
	key := digest(status)
	if err := s.store.ClaimAutomationAction(p.Profile.ID, key, "send:"+b.ID); err != nil {
		attempts, readErr := s.store.GetAutomationAttempts(p.Profile.ID, "send:"+b.ID)
		if readErr != nil {
			return nil, readErr
		}
		return map[string]any{"sent": false, "needs_user": err.Error(), "attempts": attempts}, nil
	}
	record, err := email.SendRemoval(ctx, sender, engine, p.Config.Options.Template, p.Profile, ec.From, b)
	p.LastSend = time.Now()
	if err != nil {
		return nil, err
	}
	if err := s.store.Add(record); err != nil {
		return nil, fmt.Errorf("email may have been sent but history could not be saved; review before retrying: %w", err)
	}
	state, note := "submitted", "SMTP accepted the removal request; awaiting the broker's reply."
	if record.Status != history.StatusSent {
		state, note = "unknown", "Email delivery is uncertain. Review before retrying."
	}
	if err := s.store.FinishAutomationAction(p.Profile.ID, key, state, note); err != nil {
		return nil, err
	}
	return map[string]any{"sent": record.Status == history.StatusSent, "outcome": state, "note": note, "action_key": key}, nil
}
