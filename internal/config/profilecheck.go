package config

import (
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode"
)

// ValidateProfileFields checks what a person typed into the profile form and
// returns field -> message (keys match the form's input names). It is for new
// input only: a saved config is never rejected for it, so older configs keep
// loading (docs/stability.md). Name, email and country have their own checks
// in the callers.
func ValidateProfileFields(p Profile, today time.Time) map[string]string {
	errs := map[string]string{}
	single := map[string]string{
		"first_name": p.FirstName, "middle_name": p.MiddleName, "last_name": p.LastName,
		"city": p.City, "state": p.State,
	}
	for field, v := range single {
		if msg := checkText(v, 100); msg != "" {
			errs[field] = msg
		}
	}
	if msg := checkText(p.Address, 200); msg != "" {
		errs["address"] = msg
	}
	if p.ZipCode != "" && !zipRe.MatchString(p.ZipCode) {
		errs["zip_code"] = "Use letters, digits, spaces or hyphens only (3 to 10 characters)"
	}
	if p.Phone != "" && !validPhone(p.Phone) {
		errs["phone"] = phoneMsg
	}
	for _, ph := range p.AdditionalPhones {
		if !validPhone(ph) {
			errs["additional_phones"] = fmt.Sprintf("%q: %s", ph, phoneMsg)
			break
		}
	}
	if p.DateOfBirth != "" {
		if msg := checkDOB(p.DateOfBirth, today); msg != "" {
			errs["date_of_birth"] = msg
		}
	}
	for field, lines := range map[string][]string{
		"name_variants": p.NameVariants, "previous_addresses": p.PreviousAddresses,
		"additional_emails": p.AdditionalEmails, "additional_phones": p.AdditionalPhones,
	} {
		if len(lines) > 20 {
			errs[field] = "At most 20 entries"
			continue
		}
		for _, l := range lines {
			if msg := checkText(l, 200); msg != "" {
				errs[field] = msg
				break
			}
		}
	}
	return errs
}

const phoneMsg = "Enter a phone number with 6 to 15 digits; spaces, + ( ) - . are fine"

var zipRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9 \-]{1,8}[A-Za-z0-9]$`)

// checkText rejects control characters (they would land in email headers and
// bodies) and over-long values.
func checkText(v string, max int) string {
	if len([]rune(v)) > max {
		return fmt.Sprintf("Keep this under %d characters", max)
	}
	for _, r := range v {
		if unicode.IsControl(r) {
			return "Remove the control characters"
		}
	}
	return ""
}

func validPhone(s string) bool {
	digits := 0
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9':
			digits++
		case strings.ContainsRune(" +()-./", r):
		default:
			return false
		}
	}
	return digits >= 6 && digits <= 15
}

func checkDOB(s string, today time.Time) string {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		// time.Parse says "day out of range" for 31 November and the like
		if strings.Contains(err.Error(), "out of range") {
			return "That date doesn't exist (check the day against the month)"
		}
		return "Use the format YYYY-MM-DD"
	}
	switch {
	case t.After(today):
		return "The date of birth can't be in the future"
	case t.Year() < 1900:
		return "Enter a year from 1900 on"
	}
	return ""
}
