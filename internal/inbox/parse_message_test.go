package inbox

import (
	"bytes"
	"strings"
	"testing"

	"github.com/emersion/go-imap"

	"github.com/drumandbytes/eraser/internal/broker"
)

func messageWith(raw string) (*imap.Message, *imap.BodySectionName) {
	section := &imap.BodySectionName{}
	return &imap.Message{
		Uid: 7,
		Envelope: &imap.Envelope{
			Subject:   "Re: your request",
			MessageId: "<abc@broker.example>",
			From:      []*imap.Address{{PersonalName: "Privacy Team", MailboxName: "privacy", HostName: "Broker.Example"}},
		},
		Body: map[*imap.BodySectionName]imap.Literal{section: bytes.NewReader([]byte(raw))},
	}, section
}

func TestParseMessageBodiesAndBrokerMatch(t *testing.T) {
	m := &Monitor{brokers: map[string]broker.Broker{"broker.example": {ID: "acme", Name: "Acme Data"}}}

	const both = "From: privacy@broker.example\r\nMIME-Version: 1.0\r\nContent-Type: multipart/alternative; boundary=BB\r\n\r\n" +
		"--BB\r\nContent-Type: text/plain\r\n\r\nWe deleted your data.\r\n" +
		"--BB\r\nContent-Type: text/html\r\n\r\n<p>We <b>deleted</b> your data.</p>\r\n--BB--\r\n"
	msg, section := messageWith(both)
	email, err := m.parseMessage(msg, section)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(email.Body, "We deleted your data.") || !strings.Contains(email.HTMLBody, "<b>deleted</b>") {
		t.Fatalf("plain=%q html=%q", email.Body, email.HTMLBody)
	}
	// The sender domain is matched case-insensitively to a catalog broker.
	if email.BrokerID != "acme" || email.BrokerName != "Acme Data" || email.FromDomain != "broker.example" || email.MessageID != "<abc@broker.example>" || email.FromName != "Privacy Team" {
		t.Fatalf("envelope fields: %+v", email)
	}

	const htmlOnly = "From: privacy@broker.example\r\nMIME-Version: 1.0\r\nContent-Type: text/html\r\n\r\n<p>No records found.</p>\r\n"
	msg, section = messageWith(htmlOnly)
	email, err = m.parseMessage(msg, section)
	if err != nil || email.Body != "" || !strings.Contains(email.HTMLBody, "No records found.") {
		t.Fatalf("html-only: %+v %v", email, err)
	}
}

func TestParseMessageToleratesMissingOrBrokenBodies(t *testing.T) {
	m := &Monitor{}

	// No body section at all: the envelope is still returned.
	msg := &imap.Message{Envelope: &imap.Envelope{Subject: "only headers"}}
	email, err := m.parseMessage(msg, &imap.BodySectionName{})
	if err != nil || email.Subject != "only headers" || email.Body != "" {
		t.Fatalf("no body: %+v %v", email, err)
	}

	// Not an RFC 822 message: no body, but no failure either.
	msg, section := messageWith("\x00\x01 not a mail message")
	email, err = m.parseMessage(msg, section)
	if err != nil || email.Body != "" || email.HTMLBody != "" {
		t.Fatalf("garbage: %+v %v", email, err)
	}
}
