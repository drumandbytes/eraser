package inbox

import "github.com/drumandbytes/eraser/internal/history"

// MigrateNoDataResponses uses only locally stored messages, never IMAP.
func MigrateNoDataResponses(store *history.Store) error {
	return store.MigrateNoData(func(r history.BrokerResponse) (bool, float64, bool) {
		if r.EmailBody == "" {
			t, confidence, review := ClassifyBySubjectOnly(r.EmailSubject)
			return t == ResponseNoData, confidence, review
		}
		classified := ClassifyResponse(&Email{From: r.EmailFrom, Subject: r.EmailSubject, Body: r.EmailBody})
		return classified.Type == ResponseNoData, classified.Confidence, classified.NeedsReview
	})
}
