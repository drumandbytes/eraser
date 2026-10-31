package template

import (
	"strings"
	"testing"
)

func TestFormatEML(t *testing.T) {
	got := string(FormatEML("me@example.com", "them@example.com", &Email{Subject: "Hi there", Body: "line one\nline two"}))
	for _, want := range []string{"From: me@example.com\r\n", "To: them@example.com\r\n", "Subject: Hi there\r\n", "MIME-Version: 1.0\r\n", "\r\n\r\nline one\r\nline two\r\n"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%q", want, got)
		}
	}
	if strings.Contains(string(FormatEML("", "x@y.z", &Email{Subject: "s", Body: "b\n"})), "From:") {
		t.Error("an empty sender must not produce a From header")
	}
	if got := string(FormatEML("", "x@y.z", &Email{Body: "ends with newline\n"})); strings.HasSuffix(got, "\r\n\r\n") {
		t.Error("a body already ending in a newline must not gain a blank line")
	}
}
