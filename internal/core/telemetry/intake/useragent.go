package intake

import "strings"

// The two coarse classes a User-Agent is reduced to (telemetry T1.4). The text itself is
// never recorded on the usage channel.
const (
	browserChromium = "chromium"
	browserFirefox  = "firefox"
	browserWebKit   = "webkit"
	browserOther    = "other"

	formPhone   = "phone"
	formDesktop = "desktop"
)

// maxClassifyBytes bounds the text the classifier looks at; real User-Agents are far
// shorter, and anything longer is junk that classifies as it would when cut.
const maxClassifyBytes = 512

// userAgentClass reduces a User-Agent to a browser family and a form factor (F73). It is
// pure: every input, empty or junk, maps to a token. "phone" is the Mobile token, the only
// classification that is cheap and needed (web-shell decision 21); everything else is
// "desktop". The families are the engines' vendors: Chrome, Edge, Opera and Chrome on iOS
// are "chromium"; Firefox, and Firefox on iOS, "firefox"; Safari, which names no other
// browser, "webkit".
func userAgentClass(ua string) (browser, formFactor string) {
	if len(ua) > maxClassifyBytes {
		ua = ua[:maxClassifyBytes]
	}
	formFactor = formDesktop
	if strings.Contains(ua, "Mobile") {
		formFactor = formPhone
	}
	switch {
	case strings.Contains(ua, "Firefox/"), strings.Contains(ua, "FxiOS/"):
		browser = browserFirefox
	case strings.Contains(ua, "Chrome/"), strings.Contains(ua, "Chromium/"),
		strings.Contains(ua, "CriOS/"), strings.Contains(ua, "Edg/"), strings.Contains(ua, "OPR/"):
		browser = browserChromium
	case strings.Contains(ua, "Safari/") && strings.Contains(ua, "AppleWebKit/"):
		browser = browserWebKit
	default:
		browser = browserOther
	}
	return browser, formFactor
}
