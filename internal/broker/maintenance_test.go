package broker

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func saneList(n int, extra string) string {
	var b strings.Builder
	b.WriteString("brokers:\n")
	for i := 0; i < n; i++ {
		fmt.Fprintf(&b, "  - {id: b%d, name: B%d, email: b%d@example.com, region: eu}\n", i, i, i)
	}
	b.WriteString(extra)
	return b.String()
}

func TestUpdate(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	body, etag, status := saneList(MinSaneBrokerCount, ""), `"v1"`, http.StatusOK
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("If-None-Match") == etag {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", etag)
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	embedded := CurrentCount()
	if embedded < MinSaneBrokerCount {
		t.Fatalf("embedded list has %d entries", embedded)
	}

	// checkOnly reports an update without writing it.
	res, err := Update(context.Background(), srv.URL, true)
	if err != nil || !res.Changed || res.Count != 0 {
		t.Fatalf("checkOnly = %+v, %v", res, err)
	}
	if _, err := os.Stat(UserBrokersPath()); !os.IsNotExist(err) {
		t.Fatal("checkOnly wrote the list")
	}

	res, err = Update(context.Background(), srv.URL, false)
	if err != nil || !res.Changed || res.Count != MinSaneBrokerCount || res.Before != embedded {
		t.Fatalf("update = %+v, %v", res, err)
	}
	if CurrentCount() != MinSaneBrokerCount {
		t.Errorf("CurrentCount after update = %d", CurrentCount())
	}
	if saved, _ := os.ReadFile(etagPath()); string(saved) != etag {
		t.Errorf("etag file = %q", saved)
	}

	// Same ETag: 304, nothing changes.
	if res, err := Update(context.Background(), srv.URL, false); err != nil || res.Changed {
		t.Errorf("unchanged = %+v, %v", res, err)
	}

	// A truncated download never replaces the working copy.
	body, etag = saneList(5, ""), `"v2"`
	if _, err := Update(context.Background(), srv.URL, false); err == nil || !strings.Contains(err.Error(), "refusing to replace") {
		t.Errorf("truncated list: %v", err)
	}
	if CurrentCount() != MinSaneBrokerCount {
		t.Error("a refused download changed the list")
	}

	status = http.StatusInternalServerError
	if _, err := Update(context.Background(), srv.URL, false); err == nil || !strings.Contains(err.Error(), "unexpected response") {
		t.Errorf("server error: %v", err)
	}
	if _, err := Update(context.Background(), "http://127.0.0.1:1/brokers.yaml", false); err == nil {
		t.Error("unreachable server: no error")
	}
	if _, err := Update(context.Background(), "://bad", false); err == nil || !strings.Contains(err.Error(), "bad URL") {
		t.Errorf("bad URL: %v", err)
	}
}

// ~/.eraser exists as a file: the list can't be written there.
func TestUpdateUnwritableHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.WriteFile(filepath.Join(home, ".eraser"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(saneList(MinSaneBrokerCount, "")))
	}))
	defer srv.Close()
	if _, err := Update(context.Background(), srv.URL, false); err == nil {
		t.Error("expected an error when ~/.eraser is a file")
	}
}

func TestDatabaseEdits(t *testing.T) {
	db := &BrokerDatabase{Brokers: []Broker{
		{ID: "acme", Name: " Acme Data ", Email: "Privacy@Acme.example"},
		{ID: "globex", Name: "Globex", Email: "dpo@globex.example"},
		{ID: "initech", Name: "Initech", Email: "privacy@initech.example"},
	}}

	if err := db.Add(Broker{ID: "acme"}); err == nil {
		t.Error("Add accepted a duplicate id")
	}
	if err := db.Add(Broker{ID: "umbrella", Name: "Umbrella"}); err != nil || len(db.Brokers) != 4 {
		t.Errorf("Add = %v, %d brokers", err, len(db.Brokers))
	}
	if b := db.FindByName("acme data"); b == nil || b.ID != "acme" {
		t.Errorf("FindByName = %+v", b)
	}
	if db.FindByName("nobody") != nil {
		t.Error("FindByName matched a missing name")
	}

	if b := db.RemoveByEmail("privacy@acme.EXAMPLE"); b == nil || b.ID != "acme" || db.FindByID("acme") != nil {
		t.Errorf("RemoveByEmail = %+v", b)
	}
	if db.RemoveByEmail("ghost@example.com") != nil {
		t.Error("RemoveByEmail removed a missing address")
	}
	if b := db.RemoveByID("GLOBEX"); b == nil || b.Name != "Globex" || len(db.Brokers) != 2 {
		t.Errorf("RemoveByID = %+v, %d left", b, len(db.Brokers))
	}
	if db.RemoveByID("ghost") != nil {
		t.Error("RemoveByID removed a missing id")
	}
}

func TestSaveWithBackup(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "brokers.yaml")
	db := &BrokerDatabase{Brokers: []Broker{{ID: "acme", Name: "Acme", Email: "a@acme.example", Region: "eu"}}}

	if err := db.SaveWithBackup(path); err != nil { // nothing to back up yet
		t.Fatal(err)
	}
	if _, err := os.Stat(path + ".bak"); !os.IsNotExist(err) {
		t.Error("backup made of a file that didn't exist")
	}
	first, _ := os.ReadFile(path)

	db.Brokers = append(db.Brokers, Broker{ID: "globex", Name: "Globex", Email: "g@globex.example", Region: "eu"})
	if err := db.SaveWithBackup(path); err != nil {
		t.Fatal(err)
	}
	if bak, _ := os.ReadFile(path + ".bak"); string(bak) != string(first) {
		t.Error("backup doesn't hold the previous version")
	}
	if reloaded, err := LoadFromFile(path); err != nil || len(reloaded.Brokers) != 2 {
		t.Errorf("reload = %v, %v", reloaded, err)
	}

	// The backup can't be written where a directory sits.
	if err := os.Mkdir(filepath.Join(dir, "x.yaml.bak"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "x.yaml"), first, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := db.SaveWithBackup(filepath.Join(dir, "x.yaml")); err == nil {
		t.Error("expected a backup error")
	}
	if err := db.Save(filepath.Join(dir, "missing", "b.yaml")); err == nil {
		t.Error("Save into a missing directory succeeded")
	}
	if err := db.SaveWithBackup(dir); err == nil {
		t.Error("SaveWithBackup over a directory succeeded")
	}
}

func TestParseDropsNonHTTPURLs(t *testing.T) {
	db, err := Parse([]byte(`brokers:
  - {id: a, name: A, email: a@a.example, region: eu, website: "javascript:alert(1)", opt_out_url: "ftp://a.example/x"}
  - {id: b, name: B, email: b@b.example, region: eu, website: "https://b.example", opt_out_url: "http://b.example/optout"}
  - {id: c, name: C, email: c@c.example, region: eu, website: "http://%zz"}
`))
	if err != nil {
		t.Fatal(err)
	}
	if a := db.FindByID("a"); a.Website != "" || a.OptOutURL != "" {
		t.Errorf("unsafe URLs kept: %+v", a)
	}
	if b := db.FindByID("b"); b.Website == "" || b.OptOutURL == "" {
		t.Errorf("http(s) URLs dropped: %+v", b)
	}
	if c := db.FindByID("c"); c.Website != "" {
		t.Errorf("unparseable URL kept: %+v", c)
	}
}

func TestLoadErrors(t *testing.T) {
	if _, err := LoadFromFile(filepath.Join(t.TempDir(), "nope.yaml")); err == nil {
		t.Error("LoadFromFile of a missing file succeeded")
	}

	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.MkdirAll(filepath.Join(home, ".eraser"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(LocalPath(), []byte("brokers: [not: valid: yaml"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(""); err == nil || !strings.Contains(err.Error(), "your own broker entries") {
		t.Errorf("Load with a broken local file: %v", err)
	}
	if err := SaveLocal(Broker{ID: "x"}); err == nil {
		t.Error("SaveLocal over a broken local file succeeded")
	}
}

// With no usable home directory the user paths are empty and the embedded
// list still loads.
func TestNoHomeDirectory(t *testing.T) {
	t.Setenv("HOME", "")
	if UserBrokersPath() != "" || LocalPath() != "" {
		t.Skip("this platform resolves a home directory without $HOME")
	}
	db, err := Load("")
	if err != nil || len(db.Brokers) < MinSaneBrokerCount {
		t.Errorf("Load without a home = %d brokers, %v", len(db.Brokers), err)
	}
	if local, err := LoadLocal(); err != nil || len(local.Brokers) != 0 {
		t.Errorf("LoadLocal without a home = %+v, %v", local, err)
	}
}

func TestCurrentCountBrokenUserList(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.MkdirAll(filepath.Join(home, ".eraser"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(UserBrokersPath(), []byte("brokers: [oops"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := CurrentCount(); got != 0 {
		t.Errorf("CurrentCount over a corrupt list = %d, want 0", got)
	}
}
