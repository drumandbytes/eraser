package web

import (
	"archive/zip"
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/drumandbytes/eraser/internal/broker"
)

func TestDraftsZipHasOneEMLPerReachableInScopeBroker(t *testing.T) {
	s := smokeServer(t)
	s.getConfig().Profiles[0].Country = "FR"
	s.brokers().Brokers = append(s.brokers().Brokers, broker.Broker{ID: "de-only", Name: "DE Only", Email: "dpo@de.example", Region: "eu", Countries: []string{"DE"}})

	rec := httptest.NewRecorder()
	s.setupRouter().ServeHTTP(rec, loopbackRequest(http.MethodGet, "/export/drafts.zip", nil))
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "application/zip" {
		t.Fatalf("drafts.zip = %d %q", rec.Code, rec.Header().Get("Content-Type"))
	}
	zr, err := zip.NewReader(bytes.NewReader(rec.Body.Bytes()), int64(rec.Body.Len()))
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, f := range zr.File {
		rc, _ := f.Open()
		b, _ := io.ReadAll(rc)
		_ = rc.Close()
		got[f.Name] = string(b)
	}
	if len(got) != 2 || got["spokeo.eml"] == "" || got["acme-eu.eml"] == "" {
		t.Fatalf("entries = %v (want spokeo and acme-eu only: no email-less broker, no DE-only broker for a French profile)", keysOf(got))
	}
	if !strings.Contains(got["spokeo.eml"], "To: privacy@spokeo.com\r\n") || !strings.Contains(got["spokeo.eml"], "Subject: ") {
		t.Errorf("not an RFC-822 message:\n%s", got["spokeo.eml"])
	}
}

func TestDraftsZipNeedsConfig(t *testing.T) {
	s := smokeServer(t)
	s.config.Store(nil)
	rec := httptest.NewRecorder()
	s.handleDraftsZip(rec, httptest.NewRequest(http.MethodGet, "/export/drafts.zip", nil))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("no config = %d, want 400", rec.Code)
	}
}

func keysOf(m map[string]string) []string {
	var k []string
	for name := range m {
		k = append(k, name)
	}
	return k
}
