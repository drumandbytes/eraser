package browser

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/chromedp/chromedp"

	"github.com/eraser-privacy/eraser/internal/config"
)

// isContextErr reports whether err is (or wraps) a context cancellation or
// deadline error, as opposed to chromedp's normal "not found on this page"
// signal (which shows up as a nil error with a false/empty result, not an
// error at all -- see fillSelector/fillByPattern/detectXxx). A caller of
// NavigateAndFill needs to be able to tell "this field/CAPTCHA type just
// wasn't on the page" (expected, not a bug) apart from "the browser died or
// timed out mid-fill" (a real failure) -- this is that distinction.
func isContextErr(err error) bool {
	return errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled)
}

// FormFiller handles form field detection and auto-filling
type FormFiller struct {
	profile *config.Profile
}

// FillResult contains the outcome of form filling
type FillResult struct {
	FilledFields  []string
	MissingFields []string
	Errors        []string
}

// FieldMapping maps detected field types to profile values
type FieldMapping struct {
	FieldType    string
	ProfileValue string
	Selectors    []string
	Patterns     []string // Patterns to match in name/id/placeholder
}

// NewFormFiller creates a new FormFiller
func NewFormFiller(profile *config.Profile) *FormFiller {
	return &FormFiller{profile: profile}
}

// Fill detects and fills form fields
func (f *FormFiller) Fill(ctx context.Context) *FillResult {
	result := &FillResult{}

	// Get all field mappings based on profile
	mappings := f.getFieldMappings()

	for _, mapping := range mappings {
		if mapping.ProfileValue == "" {
			result.MissingFields = append(result.MissingFields, mapping.FieldType)
			continue
		}

		filled, err := f.tryFillField(ctx, mapping)
		if err != nil {
			// A real error (e.g. context deadline/cancellation), not just
			// "this field isn't on the page" -- record it distinctly so
			// NavigateAndFill doesn't report Success=true purely because
			// some other field happened to fill before this failure.
			result.Errors = append(result.Errors, fmt.Sprintf("%s: %v", mapping.FieldType, err))
			continue
		}
		if filled {
			result.FilledFields = append(result.FilledFields, mapping.FieldType)
		}
	}

	return result
}

// getFieldMappings returns mappings for all profile fields
func (f *FormFiller) getFieldMappings() []FieldMapping {
	return []FieldMapping{
		{
			FieldType:    "email",
			ProfileValue: f.profile.Email,
			Selectors: []string{
				"input[type='email']",
				"input[name='email']",
				"input[id='email']",
				"input[name*='email']",
				"input[id*='email']",
				"input[placeholder*='email' i]",
				"input[autocomplete='email']",
			},
			Patterns: []string{"email", "e-mail", "e_mail"},
		},
		{
			FieldType:    "firstName",
			ProfileValue: f.profile.FirstName,
			Selectors: []string{
				"input[name='firstName']",
				"input[name='first_name']",
				"input[name='fname']",
				"input[id='firstName']",
				"input[id='first_name']",
				"input[name*='first']",
				"input[id*='first']",
				"input[placeholder*='first name' i]",
				"input[autocomplete='given-name']",
			},
			Patterns: []string{"first", "fname", "given"},
		},
		{
			FieldType:    "lastName",
			ProfileValue: f.profile.LastName,
			Selectors: []string{
				"input[name='lastName']",
				"input[name='last_name']",
				"input[name='lname']",
				"input[id='lastName']",
				"input[id='last_name']",
				"input[name*='last']",
				"input[id*='last']",
				"input[placeholder*='last name' i]",
				"input[autocomplete='family-name']",
			},
			Patterns: []string{"last", "lname", "family", "surname"},
		},
		{
			FieldType:    "fullName",
			ProfileValue: f.profile.FirstName + " " + f.profile.LastName,
			Selectors: []string{
				"input[name='name']",
				"input[name='fullName']",
				"input[name='full_name']",
				"input[id='name']",
				"input[id='fullName']",
				"input[placeholder*='full name' i]",
				"input[placeholder*='your name' i]",
				"input[autocomplete='name']",
			},
			Patterns: []string{"fullname", "full_name", "your_name"},
		},
		{
			FieldType:    "phone",
			ProfileValue: f.profile.Phone,
			Selectors: []string{
				"input[type='tel']",
				"input[name='phone']",
				"input[name='telephone']",
				"input[name='mobile']",
				"input[id='phone']",
				"input[name*='phone']",
				"input[id*='phone']",
				"input[placeholder*='phone' i]",
				"input[autocomplete='tel']",
			},
			Patterns: []string{"phone", "tel", "mobile", "cell"},
		},
		{
			FieldType:    "address",
			ProfileValue: f.profile.Address,
			Selectors: []string{
				"input[name='address']",
				"input[name='street']",
				"input[name='address1']",
				"input[name='streetAddress']",
				"input[id='address']",
				"input[name*='address']",
				"input[name*='street']",
				"input[placeholder*='address' i]",
				"input[placeholder*='street' i]",
				"input[autocomplete='street-address']",
				"input[autocomplete='address-line1']",
			},
			Patterns: []string{"address", "street", "addr"},
		},
		{
			FieldType:    "city",
			ProfileValue: f.profile.City,
			Selectors: []string{
				"input[name='city']",
				"input[id='city']",
				"input[name*='city']",
				"input[placeholder*='city' i]",
				"input[autocomplete='address-level2']",
			},
			Patterns: []string{"city", "town", "locality"},
		},
		{
			FieldType:    "state",
			ProfileValue: f.profile.State,
			Selectors: []string{
				"input[name='state']",
				"input[id='state']",
				"input[name*='state']",
				"input[placeholder*='state' i]",
				"select[name='state']",
				"select[id='state']",
				"input[autocomplete='address-level1']",
			},
			Patterns: []string{"state", "province", "region"},
		},
		{
			FieldType:    "zipCode",
			ProfileValue: f.profile.ZipCode,
			Selectors: []string{
				"input[name='zip']",
				"input[name='zipCode']",
				"input[name='zip_code']",
				"input[name='postalCode']",
				"input[name='postal_code']",
				"input[id='zip']",
				"input[name*='zip']",
				"input[name*='postal']",
				"input[placeholder*='zip' i]",
				"input[placeholder*='postal' i]",
				"input[autocomplete='postal-code']",
			},
			Patterns: []string{"zip", "postal", "postcode"},
		},
		{
			FieldType:    "country",
			ProfileValue: f.profile.Country,
			Selectors: []string{
				"input[name='country']",
				"input[id='country']",
				"select[name='country']",
				"select[id='country']",
				"input[autocomplete='country-name']",
			},
			Patterns: []string{"country", "nation"},
		},
		{
			FieldType:    "dateOfBirth",
			ProfileValue: f.profile.DateOfBirth,
			Selectors: []string{
				"input[name='dob']",
				"input[name='dateOfBirth']",
				"input[name='date_of_birth']",
				"input[name='birthdate']",
				"input[name='birth_date']",
				"input[type='date']",
				"input[id='dob']",
				"input[placeholder*='birth' i]",
				"input[autocomplete='bday']",
			},
			Patterns: []string{"dob", "birth", "bday"},
		},
	}
}

// tryFillField attempts to fill a field using multiple strategies. The
// returned error is non-nil only for a real failure (e.g. the browser
// context died mid-fill) -- a field simply not being present on this
// broker's page is reported as (false, nil), not an error.
func (f *FormFiller) tryFillField(ctx context.Context, mapping FieldMapping) (bool, error) {
	// Strategy 1: Try exact selectors
	for _, selector := range mapping.Selectors {
		filled, err := f.fillSelector(ctx, selector, mapping.ProfileValue)
		if err != nil {
			return false, err
		}
		if filled {
			return true, nil
		}
	}

	// Strategy 2: Try pattern matching on all input fields
	return f.fillByPattern(ctx, mapping)
}

// fillSelector attempts to fill a specific CSS selector. Returns
// (filled, err) where err is non-nil only for a real failure; a selector
// that just doesn't exist/isn't visible on this page is (false, nil).
func (f *FormFiller) fillSelector(ctx context.Context, selector string, value string) (bool, error) {
	// Check if element exists and is visible, and what tag it is (so we can
	// tell a <select> apart from a plain text input -- see fillSelectElement).
	var info struct {
		Exists bool   `json:"exists"`
		Tag    string `json:"tag"`
	}
	err := chromedp.Run(ctx, chromedp.Evaluate(
		fmt.Sprintf(`(function() {
			var el = document.querySelector("%s");
			if (el === null || el.offsetParent === null) {
				return {exists: false, tag: ""};
			}
			return {exists: true, tag: el.tagName.toLowerCase()};
		})()`, escapeSelector(selector)),
		&info,
	))

	if err != nil {
		if isContextErr(err) {
			return false, err
		}
		return false, nil
	}
	if !info.Exists {
		return false, nil
	}

	// <select> elements don't respond reliably to SendKeys -- it can return
	// a nil error while no option actually gets selected. Use a JS-based
	// approach that sets .value against a real <option> and fires 'change'.
	if info.Tag == "select" {
		return f.fillSelectElement(ctx, selector, value)
	}

	// Clear existing value and fill
	err = chromedp.Run(ctx,
		chromedp.Clear(selector),
		chromedp.SendKeys(selector, value),
	)
	if err != nil {
		if isContextErr(err) {
			return false, err
		}
		return false, nil
	}

	return true, nil
}

// fillSelectElement sets the value of a <select> element by matching value
// against an <option>'s value or visible text, setting el.value directly,
// and dispatching a 'change' event so any listeners on the page notice.
// Returns (false, nil) -- not an error -- if the select has no matching
// <option>, since that's a legitimate "couldn't fill this field" outcome
// rather than a browser/context failure.
func (f *FormFiller) fillSelectElement(ctx context.Context, selector string, value string) (bool, error) {
	js := fmt.Sprintf(`(function() {
		var el = document.querySelector("%s");
		if (!el) return false;
		var target = %s;
		var opt = Array.from(el.options).find(function(o) {
			return o.value === target || o.textContent.trim() === target;
		});
		if (!opt) return false;
		el.value = opt.value;
		el.dispatchEvent(new Event('change', {bubbles: true}));
		return el.value === opt.value;
	})()`, escapeSelector(selector), jsStringLiteral(value))

	var ok bool
	err := chromedp.Run(ctx, chromedp.Evaluate(js, &ok))
	if err != nil {
		if isContextErr(err) {
			return false, err
		}
		return false, nil
	}

	return ok, nil
}

// fillByPattern searches for fields matching patterns. Returns (filled, err)
// where err is non-nil only for a real failure (see fillSelector).
func (f *FormFiller) fillByPattern(ctx context.Context, mapping FieldMapping) (bool, error) {
	// Build JavaScript to find matching fields
	patternsJS := "["
	for i, p := range mapping.Patterns {
		if i > 0 {
			patternsJS += ","
		}
		patternsJS += fmt.Sprintf(`"%s"`, p)
	}
	patternsJS += "]"

	// JavaScript to find field by pattern
	js := fmt.Sprintf(`(function() {
		var patterns = %s;
		var inputs = document.querySelectorAll('input, select, textarea');
		for (var i = 0; i < inputs.length; i++) {
			var el = inputs[i];
			var name = (el.name || '').toLowerCase();
			var id = (el.id || '').toLowerCase();
			var placeholder = (el.placeholder || '').toLowerCase();
			var label = '';

			// Check for associated label
			if (el.id) {
				var labelEl = document.querySelector('label[for="' + el.id + '"]');
				if (labelEl) label = labelEl.textContent.toLowerCase();
			}

			for (var j = 0; j < patterns.length; j++) {
				var p = patterns[j];
				if (name.includes(p) || id.includes(p) || placeholder.includes(p) || label.includes(p)) {
					if (el.offsetParent !== null) { // is visible
						return {found: true, selector: el.tagName.toLowerCase() + (el.id ? '#' + el.id : (el.name ? '[name="' + el.name + '"]' : ''))};
					}
				}
			}
		}
		return {found: false};
	})()`, patternsJS)

	var result map[string]interface{}
	err := chromedp.Run(ctx, chromedp.Evaluate(js, &result))
	if err != nil {
		if isContextErr(err) {
			return false, err
		}
		return false, nil
	}

	found, ok := result["found"].(bool)
	if !ok || !found {
		return false, nil
	}

	selector, ok := result["selector"].(string)
	if !ok || selector == "" {
		return false, nil
	}

	return f.fillSelector(ctx, selector, mapping.ProfileValue)
}

// escapeSelector escapes special characters in CSS selectors for JS strings
func escapeSelector(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return s
}

// jsStringLiteral returns s safely embedded as a double-quoted JavaScript
// string literal (e.g. for splicing profile values into an Evaluate snippet).
func jsStringLiteral(s string) string {
	return `"` + escapeSelector(s) + `"`
}
