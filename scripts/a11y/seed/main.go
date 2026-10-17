// Seeds a throwaway HOME with a fictional profile and a spread of history so
// the accessibility scan sees every status colour, badge and task type.
// usage: go run ./scripts/a11y/seed <home-dir>
package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/drumandbytes/eraser/internal/history"
)

const cfg = `profiles:
  - id: default
    first_name: Jane
    last_name: Doe
    email: jane@example.com
    country: Latvia
email:
  provider: smtp
  from: jane@example.com
  smtp:
    host: smtp.example.com
    port: 465
    username: jane@example.com
    password: xxxxxxxxxxxxxxxx
    use_tls: true
options:
  template: gdpr
  send_mode: manual
`

func main() {
	if len(os.Args) != 2 {
		log.Fatal("usage: seed <home-dir>")
	}
	dir := filepath.Join(os.Args[1], ".eraser")
	must(os.MkdirAll(dir, 0o700))
	must(os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(cfg), 0o600))
	s, err := history.NewStore(filepath.Join(dir, "history.db"))
	must(err)
	defer func() { must(s.Close()) }()

	now := time.Now()
	rows := []struct {
		id     string
		status history.Status
		stage  history.PipelineStatus
	}{
		{"spokeo", history.StatusSent, history.PipelineAwaitingResponse},
		{"beenverified", history.StatusSent, history.PipelineAwaitingResponse},
		{"whitepages", history.StatusSent, history.PipelineConfirmed},
		{"intelius", history.StatusSent, history.PipelineConfirmed},
		{"radaris", history.StatusSent, history.PipelineNoData},
		{"peoplefinders", history.StatusSent, history.PipelineRejected},
		{"acme", history.StatusSent, history.PipelineFormRequired},
		{"globex", history.StatusFailed, history.PipelineFailed},
	}
	for _, r := range rows {
		rec := &history.Record{ProfileID: "default", BrokerID: r.id, BrokerName: r.id, Email: "privacy@" + r.id + ".test",
			Template: "gdpr", Status: r.status, SentAt: now, SentMethod: "manual"}
		if r.status == history.StatusFailed {
			rec.Error = "connection refused"
		}
		must(s.Add(rec))
		must(s.UpdatePipelineStatus("default", r.id, r.stage))
	}
	for i, t := range []history.TaskType{history.TaskManualForm, history.TaskCaptcha, history.TaskConfirm, history.TaskReview} {
		must(s.AddPendingTask(&history.PendingTask{ProfileID: "default", BrokerID: rows[6].id, BrokerName: fmt.Sprintf("Example Data Co %d", i+1),
			TaskType: t, FormURL: "https://privacy.example-data.test/optout", Status: "pending", ReviewReason: "reply is ambiguous"}))
	}
	must(s.CreateBatchApproval(history.BatchApproval{PlanID: "b7c0f3e91d2a4c58", ProfileID: "default", Digest: "d",
		Preview:   `{"profile_fields":{"first_name":"Jane"},"forms":[{"broker":"Example Data Co","url":"https://privacy.example-data.test/optout"}],"emails":[]}`,
		ExpiresAt: now.Add(15 * time.Minute)}))
}

func must(err error) {
	if err != nil {
		log.Fatal(err)
	}
}
