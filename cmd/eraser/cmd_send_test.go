package main

import (
	"strings"
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

// Email-less brokers sort first (never sent) but must not use up budget.
func TestCapSendsIgnoresEmaillessBrokers(t *testing.T) {
	brokers := []broker.Broker{{ID: "n1"}, {ID: "a", Email: "a@x"}, {ID: "n2"}, {ID: "b", Email: "b@x"}, {ID: "c", Email: "c@x"}}
	got := capSends(brokers, 2)
	var ids []string
	for _, b := range got {
		ids = append(ids, b.ID)
	}
	if strings.Join(ids, ",") != "n1,a,n2,b" {
		t.Errorf("capSends = %v, want n1,a,n2,b", ids)
	}
	if n := countWithEmail(brokers); n != 3 {
		t.Errorf("countWithEmail = %d", n)
	}
	if len(capSends(brokers, 5)) != 5 {
		t.Error("budget above the list size trimmed it")
	}
}
