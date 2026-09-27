package schedule

import (
	"strings"
	"testing"
	"time"
)

func TestTryLock(t *testing.T) {
	dir := t.TempDir()
	release, ok, err := TryLock(dir)
	if err != nil || !ok {
		t.Fatalf("first TryLock: ok=%v err=%v", ok, err)
	}
	if _, ok, err := TryLock(dir); err != nil || ok {
		t.Fatalf("second TryLock while held: ok=%v err=%v, want ok=false", ok, err)
	}
	release()
	release2, ok, err := TryLock(dir)
	if err != nil || !ok {
		t.Fatalf("TryLock after release: ok=%v err=%v", ok, err)
	}
	release2()
}

func TestStateRoundTrip(t *testing.T) {
	dir := t.TempDir()
	if s, err := LoadState(dir); err != nil || s != nil {
		t.Fatalf("LoadState with no file = %v, %v; want nil, nil", s, err)
	}
	want := State{LastRun: time.Date(2026, 9, 27, 6, 7, 0, 0, time.UTC), Mode: "os", Sent: 12, Error: "inbox: boom"}
	if err := SaveState(dir, want); err != nil {
		t.Fatal(err)
	}
	got, err := LoadState(dir)
	if err != nil || got == nil {
		t.Fatalf("LoadState: %v, %v", got, err)
	}
	if !got.LastRun.Equal(want.LastRun) || got.Mode != want.Mode || got.Sent != want.Sent || got.Error != want.Error {
		t.Fatalf("got %+v, want %+v", *got, want)
	}
}

var job = Job{Exe: "/opt/my apps/eraser", ConfigPath: `/home/u/.eraser/co"nf 100%.yaml`, LogPath: "/home/u/.eraser/auto.log"}

func TestRenderPlist(t *testing.T) {
	p := renderPlist(job)
	for _, want := range []string{
		"<string>" + launchdLabel + "</string>",
		"<string>/opt/my apps/eraser</string>",
		"<string>auto</string>", "<string>--once</string>", "<string>--config</string>",
		"<string>/home/u/.eraser/co&#34;nf 100%.yaml</string>",
		"<key>StartCalendarInterval</key>",
		"<key>Hour</key><integer>18</integer>",
		"<key>StandardOutPath</key>\n\t<string>/home/u/.eraser/auto.log</string>",
	} {
		if !strings.Contains(p, want) {
			t.Errorf("plist missing %q:\n%s", want, p)
		}
	}
}

func TestRenderSystemd(t *testing.T) {
	service, timer := renderSystemd(job)
	wantExec := `ExecStart="/opt/my apps/eraser" "auto" "--once" "--config" "/home/u/.eraser/co\"nf 100%%.yaml"`
	if !strings.Contains(service, wantExec) {
		t.Errorf("service missing %q:\n%s", wantExec, service)
	}
	for _, want := range []string{"OnCalendar=*-*-* 00/6:07:00", "Persistent=true", "WantedBy=timers.target"} {
		if !strings.Contains(timer, want) {
			t.Errorf("timer missing %q:\n%s", want, timer)
		}
	}
}
