package reclassify

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/drumandbytes/eraser/internal/history"
)

func TestRunWithoutStoreOrResponses(t *testing.T) {
	if _, err := Run(context.Background(), nil, nil, nil); err == nil {
		t.Error("a missing store must be an error")
	}
	store, err := history.NewStore(filepath.Join(t.TempDir(), "history.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	sum, err := Run(context.Background(), store, nil, nil)
	if err != nil || sum.Total != 0 {
		t.Errorf("empty history: %+v, %v", sum, err)
	}
}
