package history

import "testing"

func TestPipelineStatusOnlyMovesForward(t *testing.T) {
	s := newTestStore(t)
	if err := s.Add(&Record{ProfileID: "default", BrokerID: "acme", BrokerName: "Acme", Status: StatusSent}); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdatePipelineStatus("default", "acme", PipelineConfirmed); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdatePipelineStatus("default", "acme", PipelineAwaitingResponse); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.LatestRequests("default"); got[0].PipelineStatus != PipelineConfirmed {
		t.Fatalf("late reply moved confirmed back to %s", got[0].PipelineStatus)
	}
	if err := s.OverridePipelineStatus("default", "acme", PipelineRejected); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.LatestRequests("default"); got[0].PipelineStatus != PipelineRejected {
		t.Fatalf("human override did not apply: %s", got[0].PipelineStatus)
	}
}
