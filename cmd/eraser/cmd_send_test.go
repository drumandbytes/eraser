package main

import (
	"testing"
	"time"

	"github.com/drumandbytes/eraser/internal/broker"
	"github.com/drumandbytes/eraser/internal/history"
)

func TestFilterBrokersByStatus(t *testing.T) {
	now := time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)
	brokers := []broker.Broker{{ID: "never"}, {ID: "failed"}, {ID: "recent"}, {ID: "old"}}
	statuses := map[string]history.BrokerStatus{
		"failed": {Status: history.StatusFailed, LastSent: now.Add(-time.Hour)},
		"recent": {Status: history.StatusSent, LastSent: now.Add(-24 * time.Hour)},
		"old":    {Status: history.StatusSent, LastSent: now.Add(-26 * 24 * time.Hour)},
	}

	cases := []struct {
		status string
		want   []string
	}{
		{"eligible", []string{"never", "failed", "old"}},
		{"never", []string{"never"}},
		{"failed", []string{"failed"}},
		{"all", []string{"never", "failed", "recent", "old"}},
	}
	for _, tc := range cases {
		t.Run(tc.status, func(t *testing.T) {
			got := filterBrokersByStatus(brokers, statuses, tc.status, now)
			if len(got) != len(tc.want) {
				t.Fatalf("got %+v, want %v", got, tc.want)
			}
			for i := range got {
				if got[i].ID != tc.want[i] {
					t.Fatalf("got %+v, want %v", got, tc.want)
				}
			}
		})
	}
}
