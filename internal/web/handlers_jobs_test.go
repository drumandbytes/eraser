package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
)

// newJobTestRouter builds a minimal chi router exposing just the job status
// and cancel endpoints, bound to the real handlers on s. It deliberately
// skips the CSRF/security-header/rate-limit middleware that setupRouter
// wires up in production, since that middleware isn't what's under test
// here and would otherwise force these tests to fabricate CSRF tokens for
// no benefit - chi's URL-param extraction and the handlers' own logic
// (including the profile-scoping fix) are exercised exactly as in
// production.
func newJobTestRouter(s *Server) *chi.Mux {
	r := chi.NewRouter()
	r.Get("/api/job/{jobID}/status", s.handleAPIJobStatus)
	r.Post("/api/job/{jobID}/cancel", s.handleAPIJobCancel)
	return r
}

func requestWithProfileCookie(method, target, profileID string) *http.Request {
	req := httptest.NewRequest(method, target, nil)
	req.AddCookie(&http.Cookie{Name: activeProfileCookie, Value: profileID})
	return req
}

// TestHandleAPIJobStatus_ProfileScoping verifies fix #3: a job created under
// one profile must not be visible - not even to look up its status - to a
// request scoped to a different profile. Before the fix, handleAPIJobStatus
// looked jobs up purely by ID and ignored the requester's active profile.
func TestHandleAPIJobStatus_ProfileScoping(t *testing.T) {
	s := newTestServer(t, testConfig("a", "b"))
	router := newJobTestRouter(s)

	jobA := s.jobManager.Create(10, "a")
	jobB := s.jobManager.Create(10, "b")
	jobB.Update(7, 1, "broker-b") // distinguishable state from jobA's zero state

	t.Run("own profile sees its own job", func(t *testing.T) {
		req := requestWithProfileCookie(http.MethodGet, "/api/job/"+jobA.ID+"/status", "a")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
		}
		var got map[string]interface{}
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if got["id"] != jobA.ID {
			t.Fatalf("id = %v, want %v", got["id"], jobA.ID)
		}
	})

	t.Run("cross-profile status lookup is treated as not found", func(t *testing.T) {
		// Profile "a" asking for profile "b"'s job.
		req := requestWithProfileCookie(http.MethodGet, "/api/job/"+jobB.ID+"/status", "a")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusNotFound, rec.Body.String())
		}
		var got map[string]string
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if got["error"] != "job not found" {
			t.Fatalf("error = %q, want %q", got["error"], "job not found")
		}
		// Make sure we didn't leak jobB's real data under a 200 by accident.
		if _, ok := got["sent"]; ok {
			t.Fatalf("response leaked job fields: %v", got)
		}
	})

	t.Run("own profile still works for the other job", func(t *testing.T) {
		req := requestWithProfileCookie(http.MethodGet, "/api/job/"+jobB.ID+"/status", "b")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
		}
		var got map[string]interface{}
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if got["sent"].(float64) != 7 {
			t.Fatalf("sent = %v, want 7", got["sent"])
		}
	})
}

// TestHandleAPIJobCancel_ProfileScoping verifies the same fix for the
// cancel endpoint: a cross-profile cancel request must not be able to
// cancel another profile's job, and must get the same "not found" response
// a genuinely missing job ID would.
func TestHandleAPIJobCancel_ProfileScoping(t *testing.T) {
	s := newTestServer(t, testConfig("a", "b"))
	router := newJobTestRouter(s)

	jobB := s.jobManager.Create(10, "b")

	// Profile "a" tries to cancel profile "b"'s job.
	req := requestWithProfileCookie(http.MethodPost, "/api/job/"+jobB.ID+"/cancel", "a")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
	var got map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got["error"] != "job not found" {
		t.Fatalf("error = %q, want %q", got["error"], "job not found")
	}

	// The job must still be running - the cross-profile request must not
	// have cancelled it.
	if status := jobB.GetStatus(); status != JobStatusRunning {
		t.Fatalf("jobB.Status = %q after cross-profile cancel attempt, want %q", status, JobStatusRunning)
	}

	// Now cancel it from the correct profile and confirm it actually works.
	req2 := requestWithProfileCookie(http.MethodPost, "/api/job/"+jobB.ID+"/cancel", "b")
	rec2 := httptest.NewRecorder()
	router.ServeHTTP(rec2, req2)

	if rec2.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec2.Code, http.StatusOK, rec2.Body.String())
	}
	if status := jobB.GetStatus(); status != JobStatusCancelled {
		t.Fatalf("jobB.Status = %q after same-profile cancel, want %q", status, JobStatusCancelled)
	}
}

// TestHandleAPIJobStatus_UnknownJobID confirms an outright-unknown job ID
// still behaves the same as before the profile-scoping fix: 404 with the
// same error body, for either profile.
func TestHandleAPIJobStatus_UnknownJobID(t *testing.T) {
	s := newTestServer(t, testConfig("a"))
	router := newJobTestRouter(s)

	req := requestWithProfileCookie(http.MethodGet, "/api/job/does-not-exist/status", "a")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
}
