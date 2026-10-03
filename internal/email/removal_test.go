package email

import (
	"context"
	"net"
	"strconv"
	"strings"
	"testing"

	"github.com/drumandbytes/eraser/internal/broker"
	"github.com/drumandbytes/eraser/internal/config"
	"github.com/drumandbytes/eraser/internal/history"
	"github.com/drumandbytes/eraser/internal/template"
)

func TestSendRemoval(t *testing.T) {
	eng, err := template.NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	np := config.NamedProfile{ID: "jane", Profile: config.Profile{FirstName: "Jane", LastName: "Doe", Email: "jane@example.org"}}
	b := broker.Broker{ID: "acme", Name: "Acme", Email: "privacy@acme.example"}

	addr, data := recordingSMTPServer(t)
	host, portStr, _ := net.SplitHostPort(addr)
	port, _ := strconv.Atoi(portStr)
	s := NewSMTPSender(config.SMTPConfig{Host: host, Port: port, UseTLS: new(false)}, "jane@example.org")

	rec, err := SendRemoval(context.Background(), s, eng, "gdpr", np, "jane@example.org", b)
	if err != nil {
		t.Fatal(err)
	}
	if rec.Status != history.StatusSent || rec.ProfileID != "jane" || rec.BrokerID != "acme" || rec.Email != b.Email || rec.Template != "gdpr" || rec.MessageID == "" {
		t.Fatalf("unexpected record: %+v", rec)
	}
	if msg := <-data; !strings.Contains(msg, "Jane") {
		t.Fatalf("rendered request not sent:\n%s", msg)
	}

	// Nothing listening: a failed attempt is still a record, with the error.
	dead := NewSMTPSender(config.SMTPConfig{Host: "127.0.0.1", Port: 1}, "jane@example.org")
	rec, err = SendRemoval(context.Background(), dead, eng, "gdpr", np, "jane@example.org", b)
	if err != nil {
		t.Fatal(err)
	}
	if rec.Status != history.StatusFailed || rec.Error == "" || rec.MessageID != "" {
		t.Fatalf("failed send recorded as %+v", rec)
	}

	if _, err := SendRemoval(context.Background(), s, eng, "no-such-template", np, "jane@example.org", b); err == nil {
		t.Fatal("unknown template: want error, got nil")
	}
}
