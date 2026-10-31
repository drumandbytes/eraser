// Package reclassify re-runs the reply classifier over every saved broker
// response, first filling in message bodies that were stored without one.
// `eraser monitor --reclassify` and the web UI's Reclassify button share it.
package reclassify

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/drumandbytes/eraser/internal/broker"
	"github.com/drumandbytes/eraser/internal/config"
	"github.com/drumandbytes/eraser/internal/history"
	"github.com/drumandbytes/eraser/internal/inbox"
)

// Summary is the outcome of a run, counted by each reply's final type.
type Summary struct {
	Total, Updated, Unchanged, BodiesUpdated int

	Pending, Rejected, NoData, Success, FormRequired, ConfirmRequired, Unknown int
}

// Run reclassifies every saved response across all profiles. Inboxes are only
// used to backfill missing bodies; an unreachable one is logged and skipped.
func Run(ctx context.Context, store *history.Store, inboxes []config.InboxConfig, brokers []broker.Broker) (*Summary, error) {
	if store == nil {
		return nil, fmt.Errorf("database not available")
	}
	responses, err := store.GetAllBrokerResponses()
	if err != nil {
		return nil, fmt.Errorf("failed to get responses: %w", err)
	}
	sum := &Summary{Total: len(responses)}
	if len(responses) == 0 {
		return sum, nil
	}

	missingBodies := 0
	for _, resp := range responses {
		if resp.EmailBody == "" {
			missingBodies++
		}
	}
	if missingBodies > 0 && len(inboxes) > 0 && brokers != nil {
		log.Printf("Found %d records missing email bodies, fetching from IMAP...", missingBodies)
		sum.BodiesUpdated = backfillBodies(ctx, store, inboxes, brokers, responses)
		log.Printf("Updated %d records with email bodies from IMAP", sum.BodiesUpdated)
	}

	for _, resp := range responses {
		var newType inbox.ResponseType
		var confidence float64
		var needsReview bool
		var formURL, confirmURL string

		if resp.EmailBody != "" {
			classified := inbox.ClassifyResponse(&inbox.Email{From: resp.EmailFrom, Subject: resp.EmailSubject, Body: resp.EmailBody})
			newType, confidence, needsReview = classified.Type, classified.Confidence, classified.NeedsReview
			formURL, confirmURL = classified.FormURL, classified.ConfirmURL
		} else {
			newType, confidence, needsReview = inbox.ClassifyBySubjectOnly(resp.EmailSubject)
			formURL, confirmURL = resp.FormURL, resp.ConfirmURL
		}

		// Also refresh confidence/review flags for existing no-data outcomes.
		if string(newType) != resp.ResponseType || (newType == inbox.ResponseNoData && (confidence != resp.Confidence || needsReview != resp.NeedsReview)) {
			if err := store.UpdateBrokerResponseClassification(resp.ID, resp.ProfileID, string(newType), formURL, confirmURL, confidence, needsReview); err != nil {
				return nil, fmt.Errorf("failed to update response: %w", err)
			}
			sum.Updated++
		} else {
			sum.Unchanged++
		}
		if newType == inbox.ResponseNoData {
			if err := store.ResolveNoData(resp.ProfileID, resp.BrokerID, true); err != nil {
				return nil, fmt.Errorf("failed to reconcile response: %w", err)
			}
		}

		switch newType {
		case inbox.ResponseSuccess:
			sum.Success++
		case inbox.ResponseFormRequired:
			sum.FormRequired++
		case inbox.ResponseConfirmationRequired:
			sum.ConfirmRequired++
		case inbox.ResponseNoData:
			sum.NoData++
		case inbox.ResponseRejected:
			sum.Rejected++
		case inbox.ResponsePending:
			sum.Pending++
		default:
			sum.Unknown++
		}
	}
	return sum, nil
}

// backfillBodies fetches recent mail from every inbox (INBOX and the archive
// folder) and stores the body of each response that has none, matching on
// broker and subject. It updates responses in place and returns how many.
func backfillBodies(ctx context.Context, store *history.Store, inboxes []config.InboxConfig, brokers []broker.Broker, responses []history.BrokerResponse) int {
	var all []inbox.Email
	for _, inboxCfg := range inboxes {
		monitor := inbox.NewMonitor(inboxCfg, brokers)
		fetchCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
		if err := monitor.Connect(fetchCtx); err != nil {
			log.Printf("Warning: failed to connect to IMAP for body fetch: %v", err)
			cancel()
			continue
		}
		if emails, err := monitor.FetchBrokerEmails(fetchCtx, 30); err != nil {
			log.Printf("Warning: failed to fetch from INBOX: %v", err)
		} else {
			all = append(all, emails...)
		}
		if inboxCfg.ArchiveFolder != "" {
			if emails, err := monitor.FetchBrokerEmailsFromFolder(fetchCtx, inboxCfg.ArchiveFolder, 30); err != nil {
				log.Printf("Warning: failed to fetch from archive folder: %v", err)
			} else {
				all = append(all, emails...)
			}
		}
		_ = monitor.Disconnect()
		cancel()
	}
	log.Printf("Fetched %d emails from IMAP", len(all))

	bodies := make(map[string]string)
	for _, email := range all {
		if email.BrokerID == "" {
			continue
		}
		body := email.Body
		if body == "" {
			body = email.HTMLBody
		}
		if body != "" {
			bodies[email.BrokerID+"|"+email.Subject] = body
		}
	}

	updated := 0
	for i := range responses {
		resp := &responses[i]
		if resp.EmailBody != "" {
			continue
		}
		if body, ok := bodies[resp.BrokerID+"|"+resp.EmailSubject]; ok {
			if err := store.UpdateBrokerResponseBody(resp.ID, resp.ProfileID, body); err == nil {
				updated++
				resp.EmailBody = body
			}
		}
	}
	return updated
}
