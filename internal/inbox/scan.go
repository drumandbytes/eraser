package inbox

import (
	"context"
	"fmt"
	"log"

	"github.com/drumandbytes/eraser/internal/history"
)

// ScanOptions picks what ScanAndStore reads and how it treats replies it
// has seen before.
type ScanOptions struct {
	Days int
	// IncludeArchive also reads the archive folder, where earlier scans
	// moved replies (the web UI's scans; `eraser monitor` reads INBOX only).
	IncludeArchive bool
	// Reclassify re-runs the classifier on replies already stored and
	// updates them in place (the web UI's rescan).
	Reclassify bool
}

// ScanResult is what one ScanAndStore found.
type ScanResult struct {
	Summary  Summary              // every broker reply found, by type
	New      []ClassifiedResponse // replies stored for the first time
	Updated  int                  // stored replies reclassified (Reclassify only)
	Archived int
}

// ScanAndStore fetches broker replies from a connected inbox, classifies
// them and stores new ones in the history, attributing each to the profile
// that emailed that broker (a shared inbox serves several profiles). New
// replies also advance the broker's pipeline status; replies seen before
// don't, so a re-scan can't undo progress made since. With AutoArchive, the
// INBOX replies are moved to the archive folder afterwards.
func (m *Monitor) ScanAndStore(ctx context.Context, store *history.Store, opt ScanOptions) (ScanResult, error) {
	var res ScanResult
	emails, err := m.FetchBrokerEmails(ctx, opt.Days)
	if err != nil {
		return res, fmt.Errorf("failed to fetch emails from %s: %w", m.config.Email, err)
	}
	// Only INBOX UIDs are archived: ArchiveEmails applies them to INBOX,
	// where an archive-folder UID would name some other message.
	var inboxUIDs []uint32
	for _, e := range emails {
		if e.UID > 0 {
			inboxUIDs = append(inboxUIDs, e.UID)
		}
	}
	if opt.IncludeArchive && m.config.ArchiveFolder != "" {
		archived, err := m.FetchBrokerEmailsFromFolder(ctx, m.config.ArchiveFolder, opt.Days)
		if err != nil {
			log.Printf("Warning: failed to fetch from archive folder %s: %v", m.config.ArchiveFolder, err)
		}
		emails = append(emails, archived...)
	}

	var all []ClassifiedResponse
	for i := range emails {
		classified, outcome, err := RecordReply(store, &emails[i], opt.Reclassify)
		if err != nil {
			log.Printf("Warning: failed to store broker response for %s: %v", emails[i].BrokerID, err)
		}
		all = append(all, classified)
		switch outcome {
		case ReplyNew:
			res.New = append(res.New, classified)
		case ReplyUpdated:
			res.Updated++
		}
	}
	res.Summary = SummarizeResponses(all)

	if m.config.AutoArchive && len(inboxUIDs) > 0 {
		if err := m.EnsureFolderExists(m.config.ArchiveFolder); err != nil {
			log.Printf("Warning: could not create archive folder %s: %v", m.config.ArchiveFolder, err)
		} else if err := m.ArchiveEmails(inboxUIDs, m.config.ArchiveFolder); err != nil {
			log.Printf("Warning: could not archive emails: %v", err)
		} else {
			res.Archived = len(inboxUIDs)
		}
	}
	return res, nil
}

// ReplyOutcome is what RecordReply did with a reply.
type ReplyOutcome int

const (
	ReplySeen    ReplyOutcome = iota // already stored, left as is
	ReplyNew                         // stored for the first time
	ReplyUpdated                     // already stored, reclassified
)

// RecordReply classifies one broker reply and stores it (see ScanAndStore).
func RecordReply(store *history.Store, e *Email, reclassify bool) (ClassifiedResponse, ReplyOutcome, error) {
	classified := ClassifyResponse(e)
	body := e.Body
	if body == "" {
		body = e.HTMLBody
	}

	profileID, err := store.ResolveProfileForBroker(e.BrokerID)
	if err != nil {
		profileID = history.DefaultProfileID
	}

	if reclassify {
		existing, err := store.FindBrokerResponseBySubject(profileID, e.BrokerID, e.Subject)
		if err != nil {
			return classified, ReplySeen, err
		}
		if existing != nil {
			if err := store.UpdateBrokerResponseClassification(existing.ID, existing.ProfileID, string(classified.Type),
				classified.FormURL, classified.ConfirmURL, classified.Confidence, classified.NeedsReview); err != nil {
				return classified, ReplySeen, err
			}
			if existing.EmailBody == "" && body != "" {
				if err := store.UpdateBrokerResponseBody(existing.ID, existing.ProfileID, body); err != nil {
					return classified, ReplyUpdated, err
				}
			}
			return classified, ReplyUpdated, nil
		}
	}

	inserted, err := store.AddBrokerResponseIfNew(&history.BrokerResponse{
		ProfileID:    profileID,
		BrokerID:     e.BrokerID,
		BrokerName:   e.BrokerName,
		ResponseType: string(classified.Type),
		EmailFrom:    e.From,
		EmailSubject: e.Subject,
		EmailBody:    body,
		FormURL:      classified.FormURL,
		ConfirmURL:   classified.ConfirmURL,
		Confidence:   classified.Confidence,
		NeedsReview:  classified.NeedsReview,
		ReceivedAt:   e.ReceivedAt,
	})
	if err != nil || !inserted {
		return classified, ReplySeen, err
	}
	// No matching request row is fine: the reply still counts.
	_ = store.UpdatePipelineStatus(profileID, e.BrokerID, pipelineStatusFor(classified.Type))
	return classified, ReplyNew, nil
}

func pipelineStatusFor(t ResponseType) history.PipelineStatus {
	switch t {
	case ResponseSuccess:
		return history.PipelineConfirmed
	case ResponseFormRequired:
		return history.PipelineFormRequired
	case ResponseConfirmationRequired:
		return history.PipelineAwaitingConfirmation
	case ResponseRejected:
		return history.PipelineRejected
	default:
		return history.PipelineAwaitingResponse
	}
}
