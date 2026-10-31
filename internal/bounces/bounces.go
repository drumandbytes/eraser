// Package bounces finds broker addresses that bounced, and clears them. `eraser
// cleanup-bounces` and the web UI's bounce check share it.
package bounces

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/drumandbytes/eraser/internal/broker"
	"github.com/drumandbytes/eraser/internal/config"
	"github.com/drumandbytes/eraser/internal/inbox"
)

// Kind says what a scanned bounce turned out to be.
type Kind int

const (
	// Dead is a bounce for a broker address we really emailed.
	Dead Kind = iota
	// NoRecipient: the bounce didn't name an address we could extract.
	NoRecipient
	// NotInList: the bounced address isn't any broker's.
	NotInList
	// NeverSent: a bounce for an address we never emailed, so likely spoofed or misfiled.
	NeverSent
)

// Item is one bounce mail, in the order it was found.
type Item struct {
	Kind       Kind
	Email      string // bounced recipient ("" for NoRecipient)
	Broker     broker.Broker
	Subject    string
	ReceivedAt time.Time
}

// Report is the result of a scan.
type Report struct {
	Total int
	Items []Item
}

// Dead returns the addresses worth clearing, one per broker address.
func (r *Report) Dead() []Item {
	var out []Item
	seen := map[string]bool{}
	for _, it := range r.Items {
		if it.Kind != Dead || seen[strings.ToLower(it.Email)] {
			continue
		}
		seen[strings.ToLower(it.Email)] = true
		out = append(out, it)
	}
	return out
}

// Scan reads bounce mails from every inbox and sorts them. everSent holds the
// broker IDs that were ever really emailed: a bounce for any other address is
// not trusted.
func Scan(ctx context.Context, inboxes []config.InboxConfig, brokerDB *broker.BrokerDatabase, everSent map[string]bool, days int) (*Report, error) {
	rep := &Report{}
	for _, inboxCfg := range inboxes {
		monitor := inbox.NewMonitor(inboxCfg, brokerDB.Brokers)
		if err := monitor.Connect(ctx); err != nil {
			return nil, fmt.Errorf("failed to connect to inbox: %w", err)
		}
		emails, err := monitor.FetchBounceEmails(ctx, days)
		_ = monitor.Disconnect()
		if err != nil {
			return nil, fmt.Errorf("failed to fetch bounce emails: %w", err)
		}
		rep.Total += len(emails)
		for _, email := range emails {
			it := Item{Subject: email.Subject, ReceivedAt: email.ReceivedAt}
			recipient := inbox.ExtractBouncedRecipient(&email)
			it.Email = recipient
			switch b := brokerDB.FindByEmail(recipient); {
			case recipient == "":
				it.Kind = NoRecipient
			case b == nil:
				it.Kind = NotInList
			case !everSent[b.ID]:
				it.Kind, it.Broker = NeverSent, *b
			default:
				it.Kind, it.Broker = Dead, *b
			}
			rep.Items = append(rep.Items, it)
		}
	}
	return rep, nil
}

// Clear empties the dead addresses (never deleting the broker) and notes the
// old address. With listPath set (a list being maintained, --brokers) that file
// is edited in place with a .bak; otherwise corrections go to the user's own
// entries, which survive list updates. Returns the cleared items.
func Clear(listPath string, dead []Item) ([]Item, error) {
	var db *broker.BrokerDatabase
	var err error
	if listPath != "" {
		db, err = broker.LoadFromFile(listPath)
	} else {
		db, err = broker.Load("")
	}
	if err != nil {
		return nil, fmt.Errorf("failed to load brokers: %w", err)
	}
	var cleared []Item
	for _, it := range dead {
		b := db.MarkEmailUnreachable(it.Email, it.Subject)
		if b == nil {
			continue
		}
		if listPath == "" {
			if err := broker.SaveLocal(*b); err != nil {
				return cleared, fmt.Errorf("failed to save %s: %w", b.ID, err)
			}
		}
		cleared = append(cleared, it)
	}
	if listPath != "" {
		if err := db.SaveWithBackup(listPath); err != nil {
			return cleared, fmt.Errorf("failed to save broker database: %w", err)
		}
	}
	return cleared, nil
}
