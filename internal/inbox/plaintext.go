package inbox

import (
	"strings"

	"golang.org/x/net/html"
)

// PlainText is body with markup removed: tags become spaces, script and style
// content is dropped, entities are decoded and whitespace is collapsed. Plain
// text passes through unchanged apart from the whitespace collapse.
func PlainText(body string) string {
	z := html.NewTokenizer(strings.NewReader(body))
	var b strings.Builder
	skip := 0
	for {
		tt := z.Next()
		if tt == html.ErrorToken {
			break
		}
		if tt == html.StartTagToken || tt == html.EndTagToken {
			name, _ := z.TagName()
			if string(name) == "script" || string(name) == "style" {
				if tt == html.StartTagToken {
					skip++
				} else if skip > 0 {
					skip--
				}
			}
			b.WriteByte(' ')
		}
		if tt == html.TextToken && skip == 0 {
			b.Write(z.Text())
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}
