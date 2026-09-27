package browser

import (
	"context"

	"github.com/chromedp/chromedp"
)

// CaptchaInfo is the result of detectCaptcha.
type CaptchaInfo struct {
	Found bool
	Type  string // one of the CaptchaType constants
}

const (
	CaptchaTypeRecaptchaV2  = "recaptcha_v2"
	CaptchaTypeRecaptchaV3  = "recaptcha_v3"
	CaptchaTypeHCaptcha     = "hcaptcha"
	CaptchaTypeTurnstile    = "cloudflare_turnstile"
	CaptchaTypeFunCaptcha   = "funcaptcha"
	CaptchaTypeImageCaptcha = "image_captcha"
	CaptchaTypeTextCaptcha  = "text_captcha"
	CaptchaTypeCloudflare   = "cloudflare_challenge"
	CaptchaTypeUnknown      = "unknown"
)

// detectCaptchaJS returns the first CAPTCHA type found on the page, checked
// most specific first, or "" for none.
const detectCaptchaJS = `(function() {
	var q = function(sel) { return document.querySelector(sel); };
	var body = document.body ? document.body.innerText.toLowerCase() : '';

	if (q('iframe[src*="recaptcha"]') || q('.g-recaptcha, [data-sitekey]') || typeof grecaptcha !== 'undefined') return 'recaptcha_v2';
	var scripts = document.querySelectorAll('script[src*="recaptcha"]');
	for (var i = 0; i < scripts.length; i++) {
		if (scripts[i].src.includes('render=')) return 'recaptcha_v3';
	}
	if (q('script[src*="enterprise.js"]')) return 'recaptcha_v3';
	if (q('iframe[src*="hcaptcha"]') || q('.h-captcha, [data-hcaptcha-sitekey]') || typeof hcaptcha !== 'undefined') return 'hcaptcha';
	if (q('iframe[src*="challenges.cloudflare.com"]') || q('.cf-turnstile, [data-turnstile-sitekey]')) return 'cloudflare_turnstile';
	if (q('iframe[src*="funcaptcha"], iframe[src*="arkoselabs"]')) return 'funcaptcha';
	var fc = q('#FunCaptcha, [data-callback]');
	if (fc && fc.id && fc.id.toLowerCase().includes('funcaptcha')) return 'funcaptcha';

	var title = document.title.toLowerCase();
	if (title.includes('just a moment') || title.includes('checking your browser') || q('form#challenge-form') ||
		q('.ray-id, [data-ray]') || (body.includes('ray id') && body.includes('cloudflare'))) return 'cloudflare_challenge';

	var keywords = ['captcha', 'verification code', 'security code', 'enter the code', 'type the characters',
		'verify you are human', 'prove you are human', 'i am not a robot', 'human verification'];
	var img = q('img[src*="captcha"], img[alt*="captcha"], .captcha-image');
	var input = q('input[name*="captcha"], input[id*="captcha"], input[placeholder*="captcha" i]');
	if ((img || input) && keywords.some(function(k) { return body.includes(k); })) return input ? 'text_captcha' : 'unknown';
	if (q('img[src*="captcha"], img[alt*="captcha"]')) return 'image_captcha';
	return '';
})()`

// detectCaptcha errors only on context failure (browser died or timed out),
// never for "no CAPTCHA".
func (b *Browser) detectCaptcha(ctx context.Context) (CaptchaInfo, error) {
	var captchaType string
	if err := chromedp.Run(ctx, chromedp.Evaluate(detectCaptchaJS, &captchaType)); err != nil {
		if isContextErr(err) {
			return CaptchaInfo{}, err
		}
		return CaptchaInfo{}, nil // page script failed: treat as no CAPTCHA
	}
	return CaptchaInfo{Found: captchaType != "", Type: captchaType}, nil
}

// IsCaptchaBlocking returns true if the CAPTCHA requires human intervention.
// reCAPTCHA v3 is invisible and usually doesn't.
func (c CaptchaInfo) IsCaptchaBlocking() bool {
	return c.Found && c.Type != CaptchaTypeRecaptchaV3
}

var captchaDescriptions = map[string]string{
	CaptchaTypeRecaptchaV2:  "Google reCAPTCHA v2 - Click the checkbox and/or solve image puzzles",
	CaptchaTypeRecaptchaV3:  "Google reCAPTCHA v3 - Usually invisible, may auto-pass",
	CaptchaTypeHCaptcha:     "hCaptcha - Select images matching the description",
	CaptchaTypeTurnstile:    "Cloudflare Turnstile - Usually auto-passes after brief check",
	CaptchaTypeFunCaptcha:   "FunCaptcha - Complete interactive puzzles",
	CaptchaTypeImageCaptcha: "Image CAPTCHA - Type the characters shown in the image",
	CaptchaTypeTextCaptcha:  "Text CAPTCHA - Enter the verification code",
	CaptchaTypeCloudflare:   "Cloudflare Challenge - Wait or complete verification",
	CaptchaTypeUnknown:      "Unknown CAPTCHA type - Manual inspection required",
}

// GetCaptchaDescription returns a human-readable description
func (c CaptchaInfo) GetCaptchaDescription() string {
	if !c.Found {
		return "No CAPTCHA detected"
	}
	if desc, ok := captchaDescriptions[c.Type]; ok {
		return desc
	}
	return captchaDescriptions[CaptchaTypeUnknown]
}
