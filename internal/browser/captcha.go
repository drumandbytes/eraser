package browser

import (
	"context"

	"github.com/chromedp/chromedp"
)

// CaptchaInfo contains information about detected CAPTCHAs
type CaptchaInfo struct {
	Found       bool
	Type        string
	FrameSrc    string
	ElementID   string
	Confidence  float64
	Description string
}

// CaptchaType constants
const (
	CaptchaTypeRecaptchaV2    = "recaptcha_v2"
	CaptchaTypeRecaptchaV3    = "recaptcha_v3"
	CaptchaTypeHCaptcha       = "hcaptcha"
	CaptchaTypeTurnstile      = "cloudflare_turnstile"
	CaptchaTypeFunCaptcha     = "funcaptcha"
	CaptchaTypeImageCaptcha   = "image_captcha"
	CaptchaTypeTextCaptcha    = "text_captcha"
	CaptchaTypeCloudflare     = "cloudflare_challenge"
	CaptchaTypeUnknown        = "unknown"
)

// detectCaptcha checks the page for various CAPTCHA types. It returns an
// error only when a check hit a real failure (context deadline/cancel --
// e.g. the browser died or timed out mid-check), never for a CAPTCHA simply
// not being present, so callers can tell "no CAPTCHA" apart from "couldn't
// tell because the browser is gone".
func (b *Browser) detectCaptcha(ctx context.Context) (CaptchaInfo, error) {
	// Check for reCAPTCHA v2
	if result, err := detectRecaptchaV2(ctx); err != nil {
		return CaptchaInfo{}, err
	} else if result.Found {
		return result, nil
	}

	// Check for reCAPTCHA v3 (invisible)
	if result, err := detectRecaptchaV3(ctx); err != nil {
		return CaptchaInfo{}, err
	} else if result.Found {
		return result, nil
	}

	// Check for hCaptcha
	if result, err := detectHCaptcha(ctx); err != nil {
		return CaptchaInfo{}, err
	} else if result.Found {
		return result, nil
	}

	// Check for Cloudflare Turnstile
	if result, err := detectTurnstile(ctx); err != nil {
		return CaptchaInfo{}, err
	} else if result.Found {
		return result, nil
	}

	// Check for FunCaptcha
	if result, err := detectFunCaptcha(ctx); err != nil {
		return CaptchaInfo{}, err
	} else if result.Found {
		return result, nil
	}

	// Check for Cloudflare challenge page
	if result, err := detectCloudflareChallenge(ctx); err != nil {
		return CaptchaInfo{}, err
	} else if result.Found {
		return result, nil
	}

	// Check for generic image/text CAPTCHA
	if result, err := detectGenericCaptcha(ctx); err != nil {
		return CaptchaInfo{}, err
	} else if result.Found {
		return result, nil
	}

	return CaptchaInfo{}, nil
}

// detectRecaptchaV2 checks for Google reCAPTCHA v2
func detectRecaptchaV2(ctx context.Context) (CaptchaInfo, error) {
	js := `(function() {
		// Check for reCAPTCHA iframe
		var iframe = document.querySelector('iframe[src*="recaptcha"]');
		if (iframe) {
			return {found: true, src: iframe.src, type: 'iframe'};
		}

		// Check for reCAPTCHA div
		var div = document.querySelector('.g-recaptcha, [data-sitekey]');
		if (div) {
			return {found: true, id: div.id || '', type: 'div'};
		}

		// Check for grecaptcha object
		if (typeof grecaptcha !== 'undefined') {
			return {found: true, type: 'script'};
		}

		return {found: false};
	})()`

	var result map[string]interface{}
	err := chromedp.Run(ctx, chromedp.Evaluate(js, &result))
	if err != nil {
		if isContextErr(err) {
			return CaptchaInfo{}, err
		}
		return CaptchaInfo{}, nil
	}

	found, _ := result["found"].(bool)
	if !found {
		return CaptchaInfo{}, nil
	}

	info := CaptchaInfo{
		Found:       true,
		Type:        CaptchaTypeRecaptchaV2,
		Confidence:  0.95,
		Description: "Google reCAPTCHA v2 detected",
	}

	if src, ok := result["src"].(string); ok {
		info.FrameSrc = src
	}
	if id, ok := result["id"].(string); ok {
		info.ElementID = id
	}

	return info, nil
}

// detectRecaptchaV3 checks for Google reCAPTCHA v3 (invisible)
func detectRecaptchaV3(ctx context.Context) (CaptchaInfo, error) {
	js := `(function() {
		// reCAPTCHA v3 is typically loaded via script
		var scripts = document.querySelectorAll('script[src*="recaptcha"]');
		for (var i = 0; i < scripts.length; i++) {
			if (scripts[i].src.includes('render=')) {
				return {found: true, src: scripts[i].src};
			}
		}

		// Check for enterprise version
		scripts = document.querySelectorAll('script[src*="enterprise.js"]');
		if (scripts.length > 0) {
			return {found: true, type: 'enterprise'};
		}

		return {found: false};
	})()`

	var result map[string]interface{}
	err := chromedp.Run(ctx, chromedp.Evaluate(js, &result))
	if err != nil {
		if isContextErr(err) {
			return CaptchaInfo{}, err
		}
		return CaptchaInfo{}, nil
	}

	found, _ := result["found"].(bool)
	if !found {
		return CaptchaInfo{}, nil
	}

	return CaptchaInfo{
		Found:       true,
		Type:        CaptchaTypeRecaptchaV3,
		Confidence:  0.85,
		Description: "Google reCAPTCHA v3 (invisible) detected - may not require interaction",
	}, nil
}

// detectHCaptcha checks for hCaptcha
func detectHCaptcha(ctx context.Context) (CaptchaInfo, error) {
	js := `(function() {
		// Check for hCaptcha iframe
		var iframe = document.querySelector('iframe[src*="hcaptcha"]');
		if (iframe) {
			return {found: true, src: iframe.src};
		}

		// Check for hCaptcha div
		var div = document.querySelector('.h-captcha, [data-hcaptcha-sitekey]');
		if (div) {
			return {found: true, id: div.id || ''};
		}

		// Check for hcaptcha object
		if (typeof hcaptcha !== 'undefined') {
			return {found: true, type: 'script'};
		}

		return {found: false};
	})()`

	var result map[string]interface{}
	err := chromedp.Run(ctx, chromedp.Evaluate(js, &result))
	if err != nil {
		if isContextErr(err) {
			return CaptchaInfo{}, err
		}
		return CaptchaInfo{}, nil
	}

	found, _ := result["found"].(bool)
	if !found {
		return CaptchaInfo{}, nil
	}

	info := CaptchaInfo{
		Found:       true,
		Type:        CaptchaTypeHCaptcha,
		Confidence:  0.95,
		Description: "hCaptcha detected",
	}

	if src, ok := result["src"].(string); ok {
		info.FrameSrc = src
	}

	return info, nil
}

// detectTurnstile checks for Cloudflare Turnstile
func detectTurnstile(ctx context.Context) (CaptchaInfo, error) {
	js := `(function() {
		// Check for Turnstile iframe
		var iframe = document.querySelector('iframe[src*="challenges.cloudflare.com"]');
		if (iframe) {
			return {found: true, src: iframe.src};
		}

		// Check for Turnstile div
		var div = document.querySelector('.cf-turnstile, [data-turnstile-sitekey]');
		if (div) {
			return {found: true, id: div.id || ''};
		}

		return {found: false};
	})()`

	var result map[string]interface{}
	err := chromedp.Run(ctx, chromedp.Evaluate(js, &result))
	if err != nil {
		if isContextErr(err) {
			return CaptchaInfo{}, err
		}
		return CaptchaInfo{}, nil
	}

	found, _ := result["found"].(bool)
	if !found {
		return CaptchaInfo{}, nil
	}

	return CaptchaInfo{
		Found:       true,
		Type:        CaptchaTypeTurnstile,
		Confidence:  0.95,
		Description: "Cloudflare Turnstile detected",
	}, nil
}

// detectFunCaptcha checks for Arkose Labs FunCaptcha
func detectFunCaptcha(ctx context.Context) (CaptchaInfo, error) {
	js := `(function() {
		// Check for FunCaptcha iframe
		var iframe = document.querySelector('iframe[src*="funcaptcha"], iframe[src*="arkoselabs"]');
		if (iframe) {
			return {found: true, src: iframe.src};
		}

		// Check for FunCaptcha div
		var div = document.querySelector('#FunCaptcha, [data-callback]');
		if (div && div.id && div.id.toLowerCase().includes('funcaptcha')) {
			return {found: true, id: div.id};
		}

		return {found: false};
	})()`

	var result map[string]interface{}
	err := chromedp.Run(ctx, chromedp.Evaluate(js, &result))
	if err != nil {
		if isContextErr(err) {
			return CaptchaInfo{}, err
		}
		return CaptchaInfo{}, nil
	}

	found, _ := result["found"].(bool)
	if !found {
		return CaptchaInfo{}, nil
	}

	return CaptchaInfo{
		Found:       true,
		Type:        CaptchaTypeFunCaptcha,
		Confidence:  0.90,
		Description: "Arkose Labs FunCaptcha detected",
	}, nil
}

// detectCloudflareChallenge checks for Cloudflare challenge page
func detectCloudflareChallenge(ctx context.Context) (CaptchaInfo, error) {
	js := `(function() {
		// Check for Cloudflare challenge indicators
		var title = document.title.toLowerCase();
		if (title.includes('just a moment') || title.includes('checking your browser')) {
			return {found: true, type: 'title'};
		}

		// Check for challenge form
		var form = document.querySelector('form#challenge-form');
		if (form) {
			return {found: true, type: 'form'};
		}

		// Check for ray ID (Cloudflare identifier)
		var rayId = document.querySelector('.ray-id, [data-ray]');
		var body = document.body.innerText.toLowerCase();
		if (rayId || (body.includes('ray id') && body.includes('cloudflare'))) {
			return {found: true, type: 'rayid'};
		}

		return {found: false};
	})()`

	var result map[string]interface{}
	err := chromedp.Run(ctx, chromedp.Evaluate(js, &result))
	if err != nil {
		if isContextErr(err) {
			return CaptchaInfo{}, err
		}
		return CaptchaInfo{}, nil
	}

	found, _ := result["found"].(bool)
	if !found {
		return CaptchaInfo{}, nil
	}

	return CaptchaInfo{
		Found:       true,
		Type:        CaptchaTypeCloudflare,
		Confidence:  0.90,
		Description: "Cloudflare challenge page detected - wait or solve challenge",
	}, nil
}

// detectGenericCaptcha checks for generic image/text CAPTCHAs
func detectGenericCaptcha(ctx context.Context) (CaptchaInfo, error) {
	js := `(function() {
		var body = document.body.innerHTML.toLowerCase();
		var text = document.body.innerText.toLowerCase();

		// Check for CAPTCHA-related keywords
		var keywords = ['captcha', 'verification code', 'security code', 'enter the code',
			'type the characters', 'verify you are human', 'prove you are human',
			'i am not a robot', 'human verification'];

		for (var i = 0; i < keywords.length; i++) {
			if (text.includes(keywords[i])) {
				// Look for associated input and image
				var img = document.querySelector('img[src*="captcha"], img[alt*="captcha"], .captcha-image');
				var input = document.querySelector('input[name*="captcha"], input[id*="captcha"], input[placeholder*="captcha" i]');

				if (img || input) {
					return {found: true, keyword: keywords[i], hasImage: !!img, hasInput: !!input};
				}
			}
		}

		// Check for CAPTCHA images even without keywords
		var captchaImg = document.querySelector('img[src*="captcha"], img[alt*="captcha"]');
		if (captchaImg) {
			return {found: true, type: 'image', src: captchaImg.src};
		}

		return {found: false};
	})()`

	var result map[string]interface{}
	err := chromedp.Run(ctx, chromedp.Evaluate(js, &result))
	if err != nil {
		if isContextErr(err) {
			return CaptchaInfo{}, err
		}
		return CaptchaInfo{}, nil
	}

	found, _ := result["found"].(bool)
	if !found {
		return CaptchaInfo{}, nil
	}

	captchaType := CaptchaTypeUnknown
	if imgType, ok := result["type"].(string); ok && imgType == "image" {
		captchaType = CaptchaTypeImageCaptcha
	} else if hasInput, ok := result["hasInput"].(bool); ok && hasInput {
		captchaType = CaptchaTypeTextCaptcha
	}

	description := "Generic CAPTCHA detected"
	if keyword, ok := result["keyword"].(string); ok {
		description = "CAPTCHA detected: " + keyword
	}

	return CaptchaInfo{
		Found:       true,
		Type:        captchaType,
		Confidence:  0.75,
		Description: description,
	}, nil
}

// IsCaptchaBlocking returns true if the CAPTCHA requires human intervention
func (c CaptchaInfo) IsCaptchaBlocking() bool {
	if !c.Found {
		return false
	}

	// reCAPTCHA v3 is invisible and may not block
	if c.Type == CaptchaTypeRecaptchaV3 {
		return false
	}

	return true
}

// GetCaptchaDescription returns a human-readable description
func (c CaptchaInfo) GetCaptchaDescription() string {
	if !c.Found {
		return "No CAPTCHA detected"
	}

	descriptions := map[string]string{
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

	if desc, ok := descriptions[c.Type]; ok {
		return desc
	}

	return c.Description
}
