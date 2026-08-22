package browser

import "testing"

// IsCaptchaBlocking and GetCaptchaDescription are pure methods on an
// already-populated CaptchaInfo - unlike the detectXxx family (which drive a
// live browser via chromedp and need a real Chrome instance, see
// browser_chrome_test.go), these are cheap to check directly.

func TestCaptchaInfoIsCaptchaBlocking(t *testing.T) {
	tests := []struct {
		name string
		info CaptchaInfo
		want bool
	}{
		{"not found", CaptchaInfo{Found: false, Type: CaptchaTypeRecaptchaV2}, false},
		{"recaptcha v2 blocks", CaptchaInfo{Found: true, Type: CaptchaTypeRecaptchaV2}, true},
		{"recaptcha v3 does not block (invisible)", CaptchaInfo{Found: true, Type: CaptchaTypeRecaptchaV3}, false},
		{"hcaptcha blocks", CaptchaInfo{Found: true, Type: CaptchaTypeHCaptcha}, true},
		{"turnstile blocks", CaptchaInfo{Found: true, Type: CaptchaTypeTurnstile}, true},
		{"funcaptcha blocks", CaptchaInfo{Found: true, Type: CaptchaTypeFunCaptcha}, true},
		{"cloudflare challenge blocks", CaptchaInfo{Found: true, Type: CaptchaTypeCloudflare}, true},
		{"unknown type still blocks when found", CaptchaInfo{Found: true, Type: CaptchaTypeUnknown}, true},
		// Found=false with v3's type would be a nonsensical zero-value
		// combination, but IsCaptchaBlocking should still short-circuit on
		// Found before ever looking at Type.
		{"not found overrides type", CaptchaInfo{Found: false, Type: CaptchaTypeRecaptchaV3}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.info.IsCaptchaBlocking(); got != tt.want {
				t.Errorf("IsCaptchaBlocking() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCaptchaInfoGetCaptchaDescription(t *testing.T) {
	tests := []struct {
		name string
		info CaptchaInfo
		want string
	}{
		{"not found", CaptchaInfo{Found: false}, "No CAPTCHA detected"},
		{"recaptcha v2 has a known description", CaptchaInfo{Found: true, Type: CaptchaTypeRecaptchaV2}, "Google reCAPTCHA v2 - Click the checkbox and/or solve image puzzles"},
		{"turnstile has a known description", CaptchaInfo{Found: true, Type: CaptchaTypeTurnstile}, "Cloudflare Turnstile - Usually auto-passes after brief check"},
		{
			"unrecognized type falls back to the detector's own Description",
			CaptchaInfo{Found: true, Type: "some_future_captcha_type", Description: "custom detector description"},
			"custom detector description",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.info.GetCaptchaDescription(); got != tt.want {
				t.Errorf("GetCaptchaDescription() = %q, want %q", got, tt.want)
			}
		})
	}
}
