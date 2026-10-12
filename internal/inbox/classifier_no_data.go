package inbox

import (
	"regexp"
	"strings"
)

func emailBody(email *Email) string {
	if email.Body != "" {
		return email.Body
	}
	return email.HTMLBody
}

// A broker reporting no records is different from refusing an erasure request.
// These patterns are also used when upgrading saved replies.
var noDataPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(we\s+)?(do\s+not|don't)\s+(have|hold|store)\s+(any\s+)?(personal\s+)?(data|information|records?)\s+(about|for|on|of|relating\s+to)\s+you`),
	regexp.MustCompile(`(?i)no\s+(matching\s+)?(records?|data|information)\s+(was\s+|were\s+)?found`),
	regexp.MustCompile(`(?i)(you|your\s+(data|information|email|name|address))\s+(was\s+|were\s+|is\s+|are\s+)?not\s+(found|identified)\s+in\s+our\s+(system|database|records)`),
	regexp.MustCompile(`(?i)(do\s+not|don't)\s+have\s+(any\s+)?records?\s+(of|for|about)\s+(you|your)`),
	regexp.MustCompile(`(?i)no\s+(matching\s+)?record\s+(of\s+)?(a\s+)?report`),
	regexp.MustCompile(`(?i)(do\s+not|don't)\s+have\s+(any\s+)?record\s+of\s+(a\s+)?report`),
	regexp.MustCompile(`(?i)maintains?\s+no\s+(files?|records?|data)`),
	regexp.MustCompile(`(?i)(we\s+)?(have\s+)?no\s+data\s+(linked|associated|related)\s+to\s+(your|this)`),
	regexp.MustCompile(`(?i)has\s+never\s+existed\s+in\s+our\s+database`),
	regexp.MustCompile(`(?i)(your\s+)?(email|name|address|information)\s+was\s+not\s+identified`),
}

var subjectNoDataPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\bno\s+(matching\s+)?(records?|data)(\s+(held|found|stored))?\b`),
	regexp.MustCompile(`(?i)(deletion|removal|erasure).{0,60}not\s+found`),
	regexp.MustCompile(`(?i)unable\s+to\s+(locate|find)\s+your\s+(data|records?|information|profile)`),
}

// Conditional statements and requests for proof are not a completed search.
var uncertainNoDataPattern = regexp.MustCompile(`(?i)\b(if|unless|may|might|could|would|si|falls|wenn|peut-être|vielleicht)\b.{0,80}\b(no|not|keine|aucune)\b|\b(cannot|can't|unable\s+to)\s+(locate|find|identify|verify)\b`)
var quotedReplyPattern = regexp.MustCompile(`(?im)^\s*(on .+ wrote:|from:|sent:|de :|von:|le .+écrit\s*:|-{2,}\s*(original message|forwarded message))`)
var blockquotePattern = regexp.MustCompile(`(?is)<blockquote\b[^>]*>.*?</blockquote>`)

func anyPattern(patterns []*regexp.Regexp, text string) bool {
	for _, p := range patterns {
		if p.MatchString(text) {
			return true
		}
	}
	return false
}

// Only use the broker's reply, not a quoted request asking them to confirm
// that they hold no data. Raw content stays intact in the history database.
func noDataReplyText(content string) string {
	content = blockquotePattern.ReplaceAllString(content, "")
	if at := quotedReplyPattern.FindStringIndex(content); at != nil {
		content = content[:at[0]]
	}
	var lines []string
	for _, line := range strings.Split(content, "\n") {
		if !strings.HasPrefix(strings.TrimSpace(line), ">") {
			lines = append(lines, line)
		}
	}
	return stripHTML(strings.Join(lines, "\n"))
}

// noDataVerdict returns a separate, conservative terminal verdict. Generic
// acknowledgments and footer URLs don't outweigh a clear no-records reply;
// competing instructions, a refusal, or a deletion claim require review.
func noDataVerdict(subject, content string) (matched, review bool) {
	content = noDataReplyText(content)
	bodyMatch := anyPattern(noDataPatterns, content)
	matched = bodyMatch || anyPattern(subjectNoDataPatterns, subject)
	if !matched {
		return false, false
	}
	// An acknowledgment repeating a no-records subject is not a final answer.
	review = (!bodyMatch && (anyPattern(pendingPatterns, content) || anyPattern(subjectPendingPatterns, subject))) ||
		uncertainNoDataPattern.MatchString(content) || uncertainNoDataPattern.MatchString(subject) ||
		(content == "" && strings.Contains(strings.ToLower(subject), "not found")) ||
		anyPattern(formRequiredPatterns, content) || anyPattern(confirmationPatterns, content) ||
		anyPattern(rejectionPatterns, content) || anyPattern(successPatterns, content) ||
		anyPattern(subjectRejectionPatterns, subject) || anyPattern(subjectFormPatterns, subject) ||
		anyPattern(subjectSuccessPatterns, subject)
	return matched, review
}
