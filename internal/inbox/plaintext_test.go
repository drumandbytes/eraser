package inbox

import "testing"

func TestPlainText(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"<p>Hello&nbsp;<b>world</b> &amp; co</p>", "Hello world & co"},
		{"a<script>var x = '<b>';</script>b<style>p{}</style>c", "a b c"},
		{"plain  text\nwith\tspaces", "plain text with spaces"},
		{"1 < 2 and 3 > 2", "1 < 2 and 3 > 2"},
		{"", ""},
	} {
		if got := PlainText(tc.in); got != tc.want {
			t.Errorf("PlainText(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
