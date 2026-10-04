//go:build !race

// go-imap v1.2.1 writes IDLE's DONE from its own goroutine; -race flags it
// (upstream), so this only runs without the race detector.

package inbox

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/emersion/go-imap/client"

	"github.com/drumandbytes/eraser/internal/config"
)

// New mail during IDLE: the follow-up SELECT's unsolicited EXISTS must not
// wedge the watch. Scripted, since go-imap's server races on updates.
func TestWatchForNewEmailsSurvivesNewMail(t *testing.T) {
	reIdled := make(chan struct{})
	addr := fakeIMAPServer(t, func(conn net.Conn, br *bufio.Reader) error {
		if err := writeLines(conn, "* PREAUTH [CAPABILITY IMAP4rev1 IDLE] ready"); err != nil {
			return err
		}
		idles := 0
		for {
			tag, rest, err := readCommandLine(br)
			if err != nil {
				return err
			}
			switch cmd := strings.ToUpper(rest); {
			case strings.HasPrefix(cmd, "SELECT"):
				err = writeLines(conn, "* 2 EXISTS", "* FLAGS ()", tag+" OK [READ-WRITE] SELECT completed")
			case strings.HasPrefix(cmd, "UID SEARCH"):
				err = writeLines(conn, "* SEARCH", tag+" OK SEARCH completed")
			case cmd == "IDLE":
				idles++
				if err = writeLines(conn, "+ idling"); err != nil {
					return err
				}
				switch idles {
				case 1:
					err = writeLines(conn, "* 3 EXISTS") // new mail
				case 2:
					close(reIdled)
				}
				if err != nil {
					return err
				}
				if line, err := br.ReadString('\n'); err != nil || strings.TrimSpace(line) != "DONE" {
					return fmt.Errorf("want DONE, got %q (%v)", line, err)
				}
				err = writeLines(conn, tag+" OK IDLE terminated")
			default:
				err = writeLines(conn, tag+" OK")
			}
			if err != nil {
				return err
			}
		}
	})

	c, err := client.Dial(addr)
	if err != nil {
		t.Fatal(err)
	}
	m := &Monitor{client: c, config: config.InboxConfig{Folder: "INBOX"}}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- m.WatchForNewEmails(ctx, func(Email) {}) }()

	select {
	case <-reIdled:
	case <-time.After(10 * time.Second):
		t.Fatal("watch loop never went back to IDLE after new mail")
	}
	cancel()
	select {
	case err := <-done:
		if err != context.Canceled {
			t.Errorf("WatchForNewEmails returned %v, want context.Canceled", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("WatchForNewEmails didn't return after cancel")
	}
}
