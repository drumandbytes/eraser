package main

import (
	"fmt"

	"github.com/drumandbytes/eraser/internal/history"
	"github.com/drumandbytes/eraser/internal/inbox"
)

// All production history openings share application-level data upgrades. The
// history package remains independent of the classifier that uses it.
func openHistoryStore(path string) (*history.Store, error) {
	store, err := history.NewStore(path)
	if err != nil {
		return nil, err
	}
	if err := inbox.MigrateNoDataResponses(store); err != nil {
		_ = store.Close()
		return nil, fmt.Errorf("upgrade response classifications: %w", err)
	}
	return store, nil
}
