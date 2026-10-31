package main

import (
	"fmt"
	"time"

	"github.com/drumandbytes/eraser/internal/broker"
	"github.com/drumandbytes/eraser/internal/history"
)

// selectBrokers returns the brokers named in args (unknown IDs error), else all
// matching the filters and in scope for country. Brokers without an email are kept; callers decide.
func selectBrokers(db *broker.BrokerDatabase, args, regions, categories []string, country string) ([]broker.Broker, error) {
	if len(args) > 0 {
		out := make([]broker.Broker, 0, len(args))
		for _, id := range args {
			b := db.FindByID(id)
			if b == nil {
				return nil, fmt.Errorf("no broker with id %q (see `eraser list-brokers`)", id)
			}
			out = append(out, *b)
		}
		return out, nil
	}

	return broker.ForCountry(db.Select(nil, regions, categories, nil, nil), country), nil
}

// manualSentRecord builds the history row for a request the user sent by hand.
func manualSentRecord(profileID string, b broker.Broker, templateName string) *history.Record {
	return &history.Record{
		ProfileID:  profileID,
		BrokerID:   b.ID,
		BrokerName: b.Name,
		Email:      b.Email,
		Template:   templateName,
		Status:     history.StatusSent,
		SentAt:     time.Now(),
		SentMethod: "manual",
	}
}
