package email

import (
	"context"
	"fmt"
	"time"

	"github.com/drumandbytes/eraser/internal/broker"
	"github.com/drumandbytes/eraser/internal/config"
	"github.com/drumandbytes/eraser/internal/history"
	"github.com/drumandbytes/eraser/internal/template"
)

// SendRemoval renders the removal request for b, sends it from `from`, and
// returns the history record for the attempt (sent or failed) for the caller
// to store. A render failure returns an error and no record: nothing was
// attempted.
func SendRemoval(ctx context.Context, s *SMTPSender, eng *template.Engine, tmpl string, np config.NamedProfile, from string, b broker.Broker) (*history.Record, error) {
	rendered, err := eng.Render(tmpl, np.Profile, b)
	if err != nil {
		return nil, fmt.Errorf("failed to render template: %w", err)
	}
	result := s.Send(ctx, Message{To: b.Email, From: from, Subject: rendered.Subject, Body: rendered.Body})

	record := &history.Record{
		ProfileID:  np.ID,
		BrokerID:   b.ID,
		BrokerName: b.Name,
		Email:      b.Email,
		Template:   tmpl,
		SentAt:     time.Now(),
		Status:     history.StatusSent,
		MessageID:  result.MessageID,
	}
	if !result.Success {
		record.Status = history.StatusFailed
		record.Error = "unknown error"
		if result.Error != nil {
			record.Error = result.Error.Error()
		}
	}
	return record, nil
}
