package history

import (
	"testing"
	"time"
)

func TestTaskActorAndReviewReasonRoundTrip(t *testing.T) {
	s := newTestStore(t)
	task := &PendingTask{ProfileID: "default", BrokerID: "acme", BrokerName: "Acme", TaskType: TaskManualForm, Actor: "mcp:codex", ReviewReason: "needs_user"}
	if err := s.AddPendingTask(task); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetPendingTaskByID(task.ID, "default")
	if err != nil || got == nil {
		t.Fatalf("get: %v", err)
	}
	if got.Actor != "mcp:codex" || got.ReviewReason != "needs_user" || !got.NeedsReview() {
		t.Fatalf("task=%+v", got)
	}
	if got.ActorLabel() != "Recorded by an AI agent (codex)" {
		t.Fatalf("label=%q", got.ActorLabel())
	}
	human := PendingTask{TaskType: TaskManualForm}
	if human.ActorLabel() != "" || human.NeedsReview() {
		t.Fatal("plain form task shown as agent work or needing review")
	}
}

func TestUpdateAutomationTaskClearsReviewOnProgress(t *testing.T) {
	s := newTestStore(t)
	task, err := s.EnsureAutomationTask("default", "acme", "Acme", TaskManualForm, "https://acme.example/form", "mcp:codex")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateAutomationTask("default", task.ID, "mcp:codex", "outcome_unknown", "Unknown", "", false); err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetPendingTaskByID(task.ID, "default")
	if !got.NeedsReview() {
		t.Fatal("unknown outcome not in review")
	}
	if err := s.UpdateAutomationTask("default", task.ID, "mcp:codex", "", "Progress", "", false); err != nil {
		t.Fatal(err)
	}
	got, _ = s.GetPendingTaskByID(task.ID, "default")
	if got.NeedsReview() {
		t.Fatal("review stayed after progress")
	}
}

func TestPipelineActorIsRecordedAndExported(t *testing.T) {
	s := newTestStore(t)
	if err := s.Add(&Record{ProfileID: "default", BrokerID: "acme", BrokerName: "Acme", Status: StatusSent, SentAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdatePipelineStatusBy("default", "acme", PipelineAwaitingResponse, "mcp:codex"); err != nil {
		t.Fatal(err)
	}
	latest, _ := s.LatestRequests("default")
	if latest[0].PipelineStatus != PipelineAwaitingResponse || latest[0].PipelineActor != "mcp:codex" {
		t.Fatalf("latest=%+v", latest[0])
	}
	if err := s.OverridePipelineStatus("default", "acme", PipelineConfirmed); err != nil {
		t.Fatal(err)
	}
	latest, _ = s.LatestRequests("default")
	if latest[0].PipelineActor != "human" {
		t.Fatalf("override actor=%q", latest[0].PipelineActor)
	}
}
