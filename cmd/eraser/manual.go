package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/drumandbytes/eraser/internal/broker"
	"github.com/drumandbytes/eraser/internal/history"
	emailtmpl "github.com/drumandbytes/eraser/internal/template"
)

// selectBrokers picks the brokers a manual-mode command should act on: the
// explicit IDs in args if any were given (erroring on an unknown one), else
// every broker matching the region/category filters (all of them if both are
// empty). Brokers with no email on file are kept - the caller decides whether
// that matters (draft skips them, mark-sent still records intent).
func selectBrokers(db *broker.BrokerDatabase, args []string, region, category string) ([]broker.Broker, error) {
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

	region = strings.ToLower(strings.TrimSpace(region))
	category = strings.ToLower(strings.TrimSpace(category))
	out := make([]broker.Broker, 0, len(db.Brokers))
	for _, b := range db.Brokers {
		if region != "" && strings.ToLower(b.Region) != region {
			continue
		}
		if category != "" && strings.ToLower(b.Category) != category {
			continue
		}
		out = append(out, b)
	}
	return out, nil
}

// formatEML wraps a rendered email as an RFC-822 message that a mail client
// can open directly (double-click a .eml, or File > Import). The From line is
// the data subject's own address; there is no Date header so the client stamps
// it when the user actually sends.
func formatEML(from, to string, email *emailtmpl.Email) []byte {
	var b strings.Builder
	if from != "" {
		fmt.Fprintf(&b, "From: %s\r\n", from)
	}
	fmt.Fprintf(&b, "To: %s\r\n", to)
	fmt.Fprintf(&b, "Subject: %s\r\n", email.Subject)
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: text/plain; charset=utf-8\r\n")
	b.WriteString("\r\n")
	b.WriteString(strings.ReplaceAll(email.Body, "\n", "\r\n"))
	if !strings.HasSuffix(email.Body, "\n") {
		b.WriteString("\r\n")
	}
	return []byte(b.String())
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
