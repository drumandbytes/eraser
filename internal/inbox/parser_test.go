package inbox

import "testing"

// TestCleanURLRejectsPrivateAndLoopbackHosts covers the fix in cleanURL that
// unconditionally rejects URLs pointing at private, loopback, link-local, or
// localhost targets - the last line of defense against a broker-reply email
// (attacker-influenced content) pointing an outbound request at an internal
// service or a cloud metadata endpoint, even when the caller has disabled
// the separate domain-allowlist check.
func TestCleanURLRejectsPrivateAndLoopbackHosts(t *testing.T) {
	tests := []struct {
		name    string
		rawURL  string
		rejects bool
	}{
		// Must be rejected.
		{"cloud metadata endpoint", "http://169.254.169.254/latest/meta-data/", true},
		{"localhost by name", "http://localhost:8080/opt-out", true},
		{"IPv4 loopback", "http://127.0.0.1/opt-out", true},
		{"IPv6 loopback", "http://[::1]/opt-out", true},
		{"private range 10.x", "http://10.0.0.5/opt-out", true},
		{"private range 192.168.x", "http://192.168.1.1/opt-out", true},
		{"private range 172.16.x", "http://172.16.0.1/opt-out", true},
		{"unspecified IPv4", "http://0.0.0.0/opt-out", true},
		{"link-local IPv4", "http://169.254.1.1/opt-out", true},
		{"localhost uppercase", "http://LOCALHOST/opt-out", true},

		// Must NOT be rejected - pass through cleanURL normally.
		{"public domain", "http://example.com/opt-out", false},
		{"public domain https", "https://www.somebroker.com/privacy-request?id=1", false},
		{"public IP", "http://8.8.8.8/opt-out", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := cleanURL(tt.rawURL)
			if tt.rejects && got != "" {
				t.Errorf("cleanURL(%q) = %q, want rejected (empty string)", tt.rawURL, got)
			}
			if !tt.rejects && got == "" {
				t.Errorf("cleanURL(%q) = \"\", want it to pass through", tt.rawURL)
			}
		})
	}
}

// TestIsPrivateOrLoopbackHost tests the underlying predicate directly for
// finer-grained coverage of edge cases (bare hostnames as passed after
// url.URL.Hostname() has already stripped any port).
func TestIsPrivateOrLoopbackHost(t *testing.T) {
	tests := []struct {
		host string
		want bool
	}{
		{"localhost", true},
		{"127.0.0.1", true},
		{"127.0.0.2", true}, // whole 127.0.0.0/8 is loopback
		{"0.0.0.0", true},
		{"::1", true},
		{"10.1.2.3", true},
		{"172.16.5.5", true},
		{"172.31.255.255", true},
		{"192.168.0.1", true},
		{"169.254.169.254", true}, // cloud metadata / link-local
		{"224.0.0.1", true},       // link-local multicast

		{"example.com", false},
		{"8.8.8.8", false},
		{"1.1.1.1", false},
		{"172.32.0.1", false},    // just outside the 172.16/12 private block
		{"internal.corp", false}, // non-literal hostname: no DNS lookup performed
	}

	for _, tt := range tests {
		t.Run(tt.host, func(t *testing.T) {
			if got := isPrivateOrLoopbackHost(tt.host); got != tt.want {
				t.Errorf("isPrivateOrLoopbackHost(%q) = %v, want %v", tt.host, got, tt.want)
			}
		})
	}
}
