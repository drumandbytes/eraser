package web

import (
	"bufio"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/drumandbytes/eraser/internal/config"
	"github.com/drumandbytes/eraser/internal/email"
	"github.com/drumandbytes/eraser/internal/history"
)

// fakeRelay is a plaintext SMTP relay that accepts any number of
// connections. A recipient in reject gets a 550; authFail answers MAIL FROM
// with a 535, the way a provider that has locked the account does.
type fakeRelay struct {
	addr     string
	mu       sync.Mutex
	reject   map[string]bool
	authFail bool
	rcpts    []string
}

func newFakeRelay(t *testing.T) *fakeRelay {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	f := &fakeRelay{addr: ln.Addr().String(), reject: map[string]bool{}}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go f.serve(conn)
		}
	}()
	return f
}

func (f *fakeRelay) serve(conn net.Conn) {
	defer func() { _ = conn.Close() }()
	r := bufio.NewReader(conn)
	reply := func(s string) { _, _ = conn.Write([]byte(s + "\r\n")) }
	reply("220 fake")
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		cmd := strings.TrimSpace(line)
		upper := strings.ToUpper(cmd)
		f.mu.Lock()
		authFail, reject := f.authFail, f.reject
		f.mu.Unlock()
		switch {
		case strings.HasPrefix(upper, "MAIL") && authFail:
			reply("535 5.7.8 authentication failed")
		case strings.HasPrefix(upper, "RCPT"):
			to := strings.Trim(cmd[strings.Index(cmd, ":")+1:], "<> ")
			if reject[to] {
				reply("550 no such user")
				continue
			}
			f.mu.Lock()
			f.rcpts = append(f.rcpts, to)
			f.mu.Unlock()
			reply("250 ok")
		case strings.HasPrefix(upper, "DATA"):
			reply("354 go ahead")
			for {
				l, err := r.ReadString('\n')
				if err != nil {
					return
				}
				if l == ".\r\n" {
					break
				}
			}
			reply("250 queued")
		case strings.HasPrefix(upper, "QUIT"):
			reply("221 bye")
			return
		default:
			reply("250 ok")
		}
	}
}

func (f *fakeRelay) recipients() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.rcpts...)
}

// sendServer is smokeServer with email pointed at relay (plaintext, no auth).
func sendServer(t *testing.T, relay *fakeRelay) *Server {
	t.Helper()
	s := smokeServer(t)
	host, portStr, _ := net.SplitHostPort(relay.addr)
	port, _ := strconv.Atoi(portStr)
	noTLS := false
	cfg := *s.getConfig()
	cfg.Email = config.EmailConfig{From: "test@example.com", SMTP: config.SMTPConfig{Host: host, Port: port, UseTLS: &noTLS}}
	cfg.Options.Template = "gdpr"
	cfg.Options.RateLimitMs = 1
	s.config.Store(&cfg)
	return s
}

func waitForJob(t *testing.T, j *Job) JobStatus {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if st := j.GetStatus(); st != JobStatusRunning {
			return st
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("job still running after 10s")
	return ""
}

func post(t *testing.T, s *Server, target string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	req := loopbackRequest(http.MethodPost, target, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	s.setupRouter().ServeHTTP(rec, req)
	return rec
}

func TestSendOneDeliversAndRecords(t *testing.T) {
	relay := newFakeRelay(t)
	s := sendServer(t, relay)

	rec := post(t, s, "/api/send/spokeo", nil)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Sent") {
		t.Fatalf("send: %d %s", rec.Code, rec.Body.String())
	}
	if got := relay.recipients(); len(got) != 1 || got[0] != "privacy@spokeo.com" {
		t.Errorf("relay got %v", got)
	}
	recs, _ := s.historyStore.GetRecentRequests("default", 10)
	if len(recs) != 1 || recs[0].Status != history.StatusSent || recs[0].MessageID == "" {
		t.Errorf("history = %+v", recs)
	}
}

func TestSendOneRecordsRejectedRecipient(t *testing.T) {
	relay := newFakeRelay(t)
	relay.reject["privacy@spokeo.com"] = true
	s := sendServer(t, relay)

	rec := post(t, s, "/api/send/spokeo", nil)
	if !strings.Contains(rec.Body.String(), "Failed") {
		t.Fatalf("body = %s", rec.Body.String())
	}
	recs, _ := s.historyStore.GetRecentRequests("default", 10)
	if len(recs) != 1 || recs[0].Status != history.StatusFailed {
		t.Errorf("history = %+v", recs)
	}
}

func TestSendOneRefusals(t *testing.T) {
	relay := newFakeRelay(t)
	cases := []struct {
		name, target string
		mutate       func(s *Server)
		code         int
		want         string
	}{
		{"unknown broker", "/api/send/nope", nil, http.StatusNotFound, "Broker not found"},
		{"no config", "/api/send/spokeo", func(s *Server) { s.config.Store(nil) }, http.StatusBadRequest, "Email not configured"},
		{"no smtp", "/api/send/spokeo", func(s *Server) {
			c := *s.getConfig()
			c.Email = config.EmailConfig{}
			s.config.Store(&c)
		}, http.StatusBadRequest, "Email not configured"},
		{"dry run", "/api/send/spokeo", func(s *Server) {
			c := *s.getConfig()
			c.Options.DryRun = true
			s.config.Store(&c)
		}, http.StatusBadRequest, "dry_run"},
		{"no broker email", "/api/send/noemail", nil, http.StatusBadRequest, "No email on file"},
		{"bad provider", "/api/send/spokeo", func(s *Server) {
			c := *s.getConfig()
			c.Email.Provider = "carrier-pigeon"
			s.config.Store(&c)
		}, http.StatusOK, "unknown email provider"},
		{"bad template", "/api/send/spokeo", func(s *Server) {
			c := *s.getConfig()
			c.Options.Template = "nope"
			s.config.Store(&c)
		}, http.StatusOK, "Template error"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := sendServer(t, relay)
			if c.mutate != nil {
				c.mutate(s)
			}
			rec := post(t, s, c.target, nil)
			if rec.Code != c.code || !strings.Contains(rec.Body.String(), c.want) {
				t.Errorf("got %d %q, want %d containing %q", rec.Code, rec.Body.String(), c.code, c.want)
			}
		})
	}
	if got := relay.recipients(); len(got) != 0 {
		t.Errorf("a refused send reached the relay: %v", got)
	}
}

func TestSendOneRateLimited(t *testing.T) {
	s := sendServer(t, newFakeRelay(t))
	s.rateLimiter = NewRateLimiter(0, time.Minute)
	if rec := post(t, s, "/api/send/spokeo", nil); rec.Code != http.StatusTooManyRequests {
		t.Errorf("status = %d, want 429", rec.Code)
	}
	if rec := post(t, s, "/api/send-all", nil); rec.Code != http.StatusTooManyRequests {
		t.Errorf("send-all status = %d, want 429", rec.Code)
	}
}

func decodeJSON(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
		t.Fatalf("decode %q: %v", rec.Body.String(), err)
	}
	return m
}

func TestSendAllRunsJobToCompletion(t *testing.T) {
	relay := newFakeRelay(t)
	relay.reject["dpo@acme.example"] = true
	s := sendServer(t, relay)

	rec := post(t, s, "/api/send-all", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("send-all: %d %s", rec.Code, rec.Body.String())
	}
	body := decodeJSON(t, rec)
	if body["total"] != float64(2) { // noemail is skipped
		t.Errorf("total = %v, want 2", body["total"])
	}
	job := s.jobManager.Get(body["job_id"].(string))
	if st := waitForJob(t, job); st != JobStatusCompleted {
		t.Fatalf("job status = %s", st)
	}
	snap := readJob(t, job)
	if snap.Sent != 1 || snap.Failed != 1 {
		t.Errorf("job sent/failed = %d/%d, want 1/1", snap.Sent, snap.Failed)
	}
	if _, sent, failed, _ := s.historyStore.GetStats("default"); sent != 1 || failed != 1 {
		t.Errorf("history sent/failed = %d/%d", sent, failed)
	}

	// With spokeo now inside its cooldown and acme failed, "eligible"
	// picks only the failed one back up.
	rec = post(t, s, "/api/send-all", url.Values{"status": {"never"}})
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "No brokers matching status") {
		t.Errorf("never after a full run: %d %s", rec.Code, rec.Body.String())
	}
}

func TestSendAllRefusals(t *testing.T) {
	relay := newFakeRelay(t)
	cases := []struct {
		name   string
		form   url.Values
		mutate func(s *Server)
		code   int
		want   string
	}{
		{"no config", nil, func(s *Server) { s.config.Store(nil) }, http.StatusBadRequest, "Email not configured"},
		{"no smtp", nil, func(s *Server) {
			c := *s.getConfig()
			c.Email = config.EmailConfig{}
			s.config.Store(&c)
		}, http.StatusBadRequest, "Email not configured"},
		{"dry run", nil, func(s *Server) {
			c := *s.getConfig()
			c.Options.DryRun = true
			s.config.Store(&c)
		}, http.StatusBadRequest, "dry_run"},
		{"bad status", url.Values{"status": {"sometimes"}}, nil, http.StatusBadRequest, "Status must be"},
		{"unknown ids", url.Values{"broker_ids": {"spokeo, ghost"}}, nil, http.StatusBadRequest, "Unknown broker IDs: ghost"},
		{"nothing failed", url.Values{"status": {"failed"}}, nil, http.StatusBadRequest, "No failed brokers"},
		{"only email-less", url.Values{"broker_ids": {"noemail"}}, nil, http.StatusBadRequest, "No pending brokers"},
		{"bad provider", nil, func(s *Server) {
			c := *s.getConfig()
			c.Email.Provider = "carrier-pigeon"
			s.config.Store(&c)
		}, http.StatusInternalServerError, "unknown email provider"},
		{"active job", nil, func(s *Server) { s.jobManager.Create(1, "default") }, http.StatusConflict, "already in progress"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := sendServer(t, relay)
			if c.mutate != nil {
				c.mutate(s)
			}
			rec := post(t, s, "/api/send-all", c.form)
			if rec.Code != c.code || !strings.Contains(rec.Body.String(), c.want) {
				t.Errorf("got %d %s, want %d containing %q", rec.Code, rec.Body.String(), c.code, c.want)
			}
		})
	}
	if got := relay.recipients(); len(got) != 0 {
		t.Errorf("a refused batch reached the relay: %v", got)
	}
}

func brokersToSend(s *Server, ids ...string) []BrokerWithStatus {
	var out []BrokerWithStatus
	for _, id := range ids {
		out = append(out, BrokerWithStatus{Broker: *s.brokers().FindByID(id)})
	}
	return out
}

// Three consecutive auth failures mean the provider has locked the account:
// stop rather than burn through the rest of the list.
func TestProcessSendJobStopsOnRepeatedAuthFailures(t *testing.T) {
	relay := newFakeRelay(t)
	relay.authFail = true
	s := sendServer(t, relay)
	s.brokers().Brokers = append(s.brokers().Brokers,
		s.brokers().Brokers[0], s.brokers().Brokers[0], s.brokers().Brokers[0])
	sender, _ := email.NewSender(s.getConfig().Email)

	job := s.jobManager.Create(4, "default")
	s.processSendJob(job, brokersToSend(s, "spokeo", "spokeo", "spokeo", "spokeo"), sender)

	if job.GetStatus() != JobStatusCompleted || job.ErrorType != "auth" {
		t.Errorf("job status %s, errorType %q; want stopped for auth", job.GetStatus(), job.ErrorType)
	}
	if total, _, failed, _ := s.historyStore.GetStats("default"); total != 3 || failed != 3 {
		t.Errorf("attempts = %d (failed %d), want the 4th broker never tried", total, failed)
	}
}

// A job whose profile was deleted mid-flight falls back to the first
// profile rather than crashing; a cancelled job sends nothing.
func TestProcessSendJobProfileFallbackAndCancel(t *testing.T) {
	relay := newFakeRelay(t)
	s := sendServer(t, relay)
	sender, _ := email.NewSender(s.getConfig().Email)

	job := s.jobManager.Create(1, "deleted-profile")
	s.processSendJob(job, brokersToSend(s, "spokeo"), sender)
	if snap := readJob(t, job); snap.Sent != 1 {
		t.Errorf("fallback job = %+v", snap)
	}
	if recs, _ := s.historyStore.GetRecentRequests("default", 10); len(recs) != 1 {
		t.Errorf("send not recorded under the fallback profile: %+v", recs)
	}

	cancelled := s.jobManager.Create(1, "default")
	cancelled.Cancel()
	s.processSendJob(cancelled, brokersToSend(s, "acme-eu"), sender)
	if got := relay.recipients(); len(got) != 1 {
		t.Errorf("cancelled job still sent: %v", got)
	}
}

// A template error skips the broker as failed without recording anything.
func TestProcessSendJobTemplateError(t *testing.T) {
	s := sendServer(t, newFakeRelay(t))
	c := *s.getConfig()
	c.Options.Template = "nope"
	s.config.Store(&c)
	sender, _ := email.NewSender(c.Email)

	job := s.jobManager.Create(1, "default")
	s.processSendJob(job, brokersToSend(s, "spokeo"), sender)
	if snap := readJob(t, job); snap.Failed != 1 || snap.Sent != 0 {
		t.Errorf("job = %+v", snap)
	}
	if total, _, _, _ := s.historyStore.GetStats("default"); total != 0 {
		t.Errorf("template error recorded %d rows", total)
	}
}

func TestRecordSendWithoutOrBrokenStore(t *testing.T) {
	s := newTestServer(t, testConfig())
	s.recordSend(&history.Record{BrokerID: "x"}) // nil store: no panic

	s = smokeServer(t)
	_ = s.historyStore.Close()
	s.recordSend(&history.Record{BrokerID: "x"}) // write error is logged, not fatal
}

func TestJobLifecycleHelpers(t *testing.T) {
	jm := &JobManager{jobs: map[string]*Job{}}
	if jm.AnyActive() {
		t.Fatal("empty manager reports an active job")
	}
	j := jm.Create(5, "p")
	if !jm.AnyActive() || j.Context() == nil {
		t.Fatal("new job not active or has no context")
	}

	if j.RecordAuthFailure() || j.RecordAuthFailure() {
		t.Fatal("stopped before the third auth failure")
	}
	j.ResetAuthFailures()
	if j.RecordAuthFailure() {
		t.Fatal("reset didn't clear the counter")
	}

	j.StopWithError("auth", "locked out")
	if j.GetStatus() != JobStatusCompleted || j.Error != "locked out" || jm.AnyActive() {
		t.Fatalf("after StopWithError: %+v", j)
	}

	running := jm.Create(1, "p")
	jm.Cleanup(time.Hour)
	if jm.Get(j.ID) == nil {
		t.Error("Cleanup evicted a job that finished just now")
	}
	jm.Cleanup(-time.Hour) // cutoff in the future: everything finished is old
	if jm.Get(j.ID) != nil {
		t.Error("Cleanup kept an expired finished job")
	}
	if jm.Get(running.ID) == nil {
		t.Error("Cleanup evicted a running job")
	}
}

func TestJobActiveEndpoint(t *testing.T) {
	s := smokeServer(t)
	router := s.setupRouter()
	get := func() map[string]any {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, loopbackRequest(http.MethodGet, "/api/job/active", nil))
		return decodeJSON(t, rec)
	}
	if got := get(); got["job"] != nil {
		t.Fatalf("idle: %v", got)
	}
	job := s.jobManager.Create(3, "default")
	got := get()
	if inner, _ := got["job"].(map[string]any); inner == nil || inner["id"] != job.ID {
		t.Errorf("active: %v", got)
	}
}
