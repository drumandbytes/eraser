package main

import (
	"testing"
	"time"

	"github.com/drumandbytes/eraser/internal/history"
)

func TestMCPApproveAndPending(t *testing.T) {
	orig := stdinIsTerminal
	t.Cleanup(func() { stdinIsTerminal = orig })
	stdinIsTerminal = func() bool { return true }
	e := newCLIEnv(t, manualConfig)
	store, err := history.NewStore(history.DBPathFor(e.cfgPath))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.CreateBatchApproval(history.BatchApproval{PlanID: "plan-1", ProfileID: "default", Digest: "d", Preview: `{"emails":[]}`, ExpiresAt: time.Now().Add(time.Minute)}); err != nil {
		t.Fatal(err)
	}
	_ = store.Close()

	out, err := e.run(t, "", "mcp", "pending")
	if err != nil {
		t.Fatalf("pending: %v\n%s", err, out)
	}
	mustContain(t, out, "plan-1", "profile=default")

	stdinIsTerminal = func() bool { return false }
	if _, err := e.run(t, "", "mcp", "approve", "plan-1"); err == nil {
		t.Fatal("approved without a terminal")
	}
	stdinIsTerminal = func() bool { return true }
	out, err = e.run(t, "", "mcp", "approve", "plan-1")
	if err != nil {
		t.Fatalf("approve: %v\n%s", err, out)
	}
	mustContain(t, out, "Approved batch plan-1")

	out, err = e.run(t, "", "mcp", "pending")
	if err != nil {
		t.Fatalf("pending after approve: %v\n%s", err, out)
	}
	mustContain(t, out, "No batches waiting for approval.")

	if _, err := e.run(t, "", "mcp", "approve", "plan-1"); err == nil {
		t.Fatal("approved the same batch twice")
	}
	if _, err := e.run(t, "", "mcp", "approve", "unknown"); err == nil {
		t.Fatal("approved a batch that does not exist")
	}
}
