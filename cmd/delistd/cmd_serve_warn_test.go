package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestWarnNonLoopback(t *testing.T) {
	for _, listen := range []string{"127.0.0.1", "localhost", "::1"} {
		var out bytes.Buffer
		warnNonLoopback(&out, listen)
		if out.Len() != 0 {
			t.Errorf("%s: unexpected warning %q", listen, out.String())
		}
	}
	var out bytes.Buffer
	warnNonLoopback(&out, "0.0.0.0")
	if !strings.Contains(out.String(), "0.0.0.0") || !strings.Contains(out.String(), "no login") {
		t.Errorf("0.0.0.0: warning = %q", out.String())
	}
}
