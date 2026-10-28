package main

import (
	"strings"
	"testing"

	"github.com/drumandbytes/eraser/internal/smtptest"
)

func customRelayConfig(rl *smtptest.Relay, body string) string {
	cfg := strings.Replace(relayConfig(rl, ""), "template: gdpr", "template: custom", 1)
	return cfg + "  custom_template: |\n    " + strings.ReplaceAll(strings.TrimSpace(body), "\n", "\n    ") + "\n"
}

func TestCustomTemplateUsedByDraft(t *testing.T) {
	rl := smtptest.Start(t)
	e := newCLIEnv(t, customRelayConfig(rl, "Subject: Erase me please\n\nDear {{.BrokerName}}, from {{.FullName}}"))
	out, err := e.run(t, "", "--profile", "default", "draft", "acme")
	if err != nil {
		t.Fatal(err)
	}
	mustContain(t, out, "Erase me please", "Dear Acme Data, from Jane Doe")
}

func TestInvalidCustomTemplateFailsCommands(t *testing.T) {
	rl := smtptest.Start(t)
	e := newCLIEnv(t, customRelayConfig(rl, "Dear {{.Nope"))
	for _, args := range [][]string{
		{"draft", "acme"},
		{"send", "--dry-run"},
		{"send", "--manual"},
		{"export", "-o", t.TempDir() + "/ev.html"},
		{"serve"},
	} {
		_, err := e.run(t, "", append([]string{"--profile", "default"}, args...)...)
		if err == nil || !strings.Contains(err.Error(), "custom template") {
			t.Errorf("%v: err = %v, want a custom template error", args, err)
		}
	}
}
