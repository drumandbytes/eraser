package broker

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// DefaultUpdateURL is where `update-brokers` and the web UI's update button
// fetch the current list from. Every 1.x release reads it, so the format may
// only gain optional fields (docs/stability.md).
const DefaultUpdateURL = "https://raw.githubusercontent.com/drumandbytes/eraser/main/data/brokers.yaml"

// UpdateResult reports what Update did. Changed is false when the server
// answered 304 (nothing new); with checkOnly, Changed means "an update is
// available" and nothing was written.
type UpdateResult struct {
	Changed bool
	Before  int // entries in the published list before the update
	Count   int // entries in the downloaded list (0 unless written)
	Own     int // the user's own entries (LocalPath), kept across updates
	Path    string
}

func etagPath() string {
	return filepath.Join(filepath.Dir(UserBrokersPath()), "brokers.etag")
}

// CurrentCount is the size of the published list (without the user's own
// entries), 0 if it can't load.
func CurrentCount() int {
	db, err := loadPublished()
	if err != nil {
		return 0
	}
	return len(db.Brokers)
}

// Update fetches url into UserBrokersPath, conditionally (If-None-Match on the
// last ETag), and only after Validate accepts it - a truncated or malformed
// download never replaces the working copy.
func Update(ctx context.Context, url string, checkOnly bool) (UpdateResult, error) {
	res := UpdateResult{Before: CurrentCount(), Path: UserBrokersPath()}
	if local, err := LoadLocal(); err == nil {
		res.Own = len(local.Brokers)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return res, fmt.Errorf("bad URL: %w", err)
	}
	if saved, _ := os.ReadFile(etagPath()); len(saved) > 0 {
		req.Header.Set("If-None-Match", strings.TrimSpace(string(saved)))
	}
	req.Header.Set("User-Agent", "eraser-update-brokers")

	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return res, fmt.Errorf("request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	switch resp.StatusCode {
	case http.StatusNotModified:
		return res, nil
	case http.StatusOK:
	default:
		return res, fmt.Errorf("unexpected response: %s", resp.Status)
	}
	res.Changed = true
	if checkOnly {
		return res, nil
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return res, fmt.Errorf("failed to read response: %w", err)
	}
	db, err := Validate(body, MinSaneBrokerCount)
	if err != nil {
		return res, fmt.Errorf("refusing to replace the local copy - %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(res.Path), 0o700); err != nil {
		return res, fmt.Errorf("failed to create %s: %w", filepath.Dir(res.Path), err)
	}
	if err := os.WriteFile(res.Path, body, 0o644); err != nil {
		return res, fmt.Errorf("failed to write %s: %w", res.Path, err)
	}
	if etag := resp.Header.Get("ETag"); etag != "" {
		_ = os.WriteFile(etagPath(), []byte(etag), 0o644)
	}
	res.Count = len(db.Brokers)
	return res, nil
}
