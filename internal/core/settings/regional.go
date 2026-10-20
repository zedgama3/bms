package settings

import (
	"errors"
	"fmt"
	"math"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/zedgama3/bms/internal/model"
)

// regionalArea is the name of the area this package declares and reserves.
const regionalArea = "regional"

// The keys of the regional area, bare.
const (
	keyName           = "name"
	keyContactEmail   = "contact_email"
	keyPublicURL      = "public_url"
	keyLanguage       = "language"
	keyTimezone       = "timezone"
	keyColorPrimary   = "color_primary"
	keyColorPrimaryDk = "color_primary_dark"
)

// The shipped backgrounds a brand colour must contrast with (web-shell tokens).
const (
	lightBackground = "#ffffff"
	darkBackground  = "#121212"

	maxTimezoneBytes = 64
	defaultName      = "BMS"
	hoverShift       = 0.12 // how far the hover colour moves, in sRGB
)

// regionalDecls are the regional's own settings. All are public (decision 6).
func regionalDecls() []Decl {
	return []Decl{
		{Key: keyName, Kind: Text, Default: defaultName, Check: checkName},
		{Key: keyContactEmail, Kind: Text, Check: checkContactEmail},
		{Key: keyPublicURL, Kind: Text, Check: checkPublicURL},
		{Key: keyLanguage, Kind: Enum, Default: "en", Options: slices.Clone(Languages)},
		{Key: keyTimezone, Kind: Text, Check: checkTimezone},
		{Key: keyColorPrimary, Kind: Text, Check: checkColor(lightBackground)},
		{Key: keyColorPrimaryDk, Kind: Text, Check: checkColor(darkBackground)},
	}
}

// regionalNormalizers rewrite a trimmed value before it is checked and stored.
var regionalNormalizers = map[string]func(string) string{
	regionalArea + "." + keyPublicURL:      canonicalPublicURLOrSame,
	regionalArea + "." + keyColorPrimary:   strings.ToLower,
	regionalArea + "." + keyColorPrimaryDk: strings.ToLower,
}

// canonicalPublicURLOrSame is the normalizer of regional.public_url: the canonical form of a
// valid value, and v unchanged otherwise, so the Check that follows reports why.
func canonicalPublicURLOrSame(v string) string {
	if c, err := canonicalPublicURL(v); err == nil {
		return c
	}
	return v
}

func checkName(v string) error {
	switch {
	case v == "":
		return fieldError("", codeRequired, nil, "must not be empty")
	case strings.IndexFunc(v, unicode.IsControl) >= 0:
		return fieldError("", codeControl, nil, "must not contain control characters")
	case len(v) > MaxNameBytes:
		p := tooLong(MaxNameBytes)
		return fieldError("", p.code, p.params, p.reason)
	}
	return nil
}

func checkContactEmail(v string) error {
	if v == "" {
		return nil
	}
	if _, err := model.ParseEmail(v); err != nil {
		return fieldError("", codeMalformed, nil, "must be a valid e-mail address")
	}
	return nil
}

// The values of the reason param of a refused regional.public_url (code url).
const (
	urlReasonScheme   = "scheme"
	urlReasonHost     = "host"
	urlReasonUserinfo = "userinfo"
	urlReasonQuery    = "query"
	urlReasonFragment = "fragment"
	urlReasonChars    = "chars"
)

// checkPublicURL is the Check of regional.public_url (settings.md, "regional.public_url
// canonical form"): "" or an absolute http(s) URL with a host, an optional port in 1..65535,
// no userinfo, no query ("?") and no fragment ("#"), and no space, control or Unicode format character. A
// value over MaxURLBytes is too_long; every other refusal is code url with a reason param.
func checkPublicURL(v string) error {
	_, err := parsePublicURL(v)
	return err
}

// canonicalPublicURL returns the stored form of a valid regional.public_url: lowercase
// scheme, lowercase host (and port), the path as given and every trailing "/" removed. It
// fails as checkPublicURL does; "" is valid and stays "".
func canonicalPublicURL(v string) (string, error) { return parsePublicURL(v) }

// parsePublicURL validates v and returns its canonical form.
func parsePublicURL(v string) (string, error) {
	if v == "" {
		return "", nil
	}
	if len(v) > MaxURLBytes {
		p := tooLong(MaxURLBytes)
		return "", fieldError("", p.code, p.params, p.reason)
	}
	bad := func(reason, why string) (string, error) {
		params := map[string]string{"reason": reason}
		if reason == urlReasonScheme {
			params["schemes"] = "https, http"
		}
		return "", fieldError("", codeURL, params, why)
	}
	if strings.IndexFunc(v, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) || unicode.Is(unicode.Cf, r) }) >= 0 {
		return bad(urlReasonChars, "must not contain a space, a control character or an invisible format character")
	}
	if strings.Contains(v, "?") {
		return bad(urlReasonQuery, "must not contain a query")
	}
	if strings.Contains(v, "#") {
		return bad(urlReasonFragment, "must not contain a fragment")
	}
	u, err := url.Parse(v)
	if err != nil {
		var esc url.EscapeError
		if errors.As(err, &esc) {
			return bad(urlReasonChars, "must be a valid URL")
		}
		return bad(urlReasonHost, "must be a valid URL")
	}
	switch {
	case u.Scheme != "https" && u.Scheme != "http": // url.Parse lowercases the scheme
		return bad(urlReasonScheme, "must start with https:// or http://")
	case u.User != nil:
		return bad(urlReasonUserinfo, "must not contain a user name or password")
	case u.Hostname() == "" || strings.HasSuffix(u.Host, ":"):
		return bad(urlReasonHost, "must name a host")
	}
	if port := u.Port(); port != "" {
		if n, err := strconv.Atoi(port); err != nil || n < 1 || n > 65535 {
			return bad(urlReasonHost, "the port must be 1 to 65535")
		}
	}
	// The authority and the path come from the raw text, not from u (whose Host is already
	// unescaped, so "%25eth0" or "%C3%A9" would not survive): only the scheme and host change
	// case, so canonicalising a canonical value changes nothing.
	rest := v[len(u.Scheme)+len("://"):]
	authority, path := rest, ""
	if i := strings.IndexByte(rest, '/'); i >= 0 {
		authority, path = rest[:i], rest[i:]
	}
	return u.Scheme + "://" + strings.ToLower(authority) + strings.TrimRight(path, "/"), nil
}

func checkTimezone(v string) error {
	switch {
	case v == "":
		return nil
	case len(v) > maxTimezoneBytes:
		p := tooLong(maxTimezoneBytes)
		return fieldError("", p.code, p.params, p.reason)
	case v == "Local":
		return fieldError("", codeUnknown, nil, "must be an IANA time zone name")
	}
	if _, err := time.LoadLocation(v); err != nil {
		return fieldError("", codeUnknown, nil, "must be an IANA time zone name")
	}
	return nil
}

// checkColor returns the Check of a brand colour: "" or #rrggbb with at least MinContrast
// against background.
func checkColor(background string) func(string) error {
	bg, _ := parseHex(background)
	return func(v string) error {
		if v == "" {
			return nil
		}
		c, ok := parseHex(v)
		if !ok {
			return fieldError("", codeMalformed, nil, "must be a colour such as #9a3412")
		}
		if ratio := contrast(c, bg); ratio < MinContrast {
			shown := formatRatio(ratio)
			return fieldError("", codeContrast,
				map[string]string{"ratio": shown, "min": strconv.FormatFloat(MinContrast, 'f', 1, 64)},
				fmt.Sprintf("contrast %s:1 is below %.1f:1", shown, MinContrast))
		}
		return nil
	}
}

// formatRatio renders a contrast ratio rounded down to two decimals, so a ratio that fails
// never reads as "4.50" (the minimum) after rounding up.
func formatRatio(r float64) string {
	return fmt.Sprintf("%.2f", math.Floor(r*100+1e-9)/100)
}

// ---- colours -----------------------------------------------------------------------------

type rgb struct{ r, g, b uint8 }

// parseHex reads #rrggbb (either case). Cannot fail beyond ok.
func parseHex(s string) (rgb, bool) {
	if len(s) != 7 || s[0] != '#' {
		return rgb{}, false
	}
	var c [3]uint8
	for i := range c {
		n, err := strconv.ParseUint(s[1+2*i:3+2*i], 16, 8)
		if err != nil || !isHexPair(s[1+2*i:3+2*i]) {
			return rgb{}, false
		}
		c[i] = uint8(n)
	}
	return rgb{c[0], c[1], c[2]}, true
}

// isHexPair rejects what ParseUint accepts and a colour must not contain (a sign).
func isHexPair(s string) bool {
	for _, r := range s {
		if !strings.ContainsRune("0123456789abcdefABCDEF", r) {
			return false
		}
	}
	return true
}

func (c rgb) hex() string { return fmt.Sprintf("#%02x%02x%02x", c.r, c.g, c.b) }

// luminance is the WCAG 2.2 relative luminance.
func (c rgb) luminance() float64 {
	lin := func(v uint8) float64 {
		s := float64(v) / 255
		if s <= 0.03928 {
			return s / 12.92
		}
		return math.Pow((s+0.055)/1.055, 2.4)
	}
	return 0.2126*lin(c.r) + 0.7152*lin(c.g) + 0.0722*lin(c.b)
}

// contrast is the WCAG contrast ratio of two colours, 1..21.
func contrast(a, b rgb) float64 {
	la, lb := a.luminance(), b.luminance()
	if la < lb {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05)
}

// mix moves c toward target by f (0..1) in sRGB.
func (c rgb) mix(target uint8, f float64) rgb {
	m := func(v uint8) uint8 {
		return uint8(math.Round(float64(v) + (float64(target)-float64(v))*f))
	}
	return rgb{m(c.r), m(c.g), m(c.b)}
}

// foreground is white or #1a1a1a, whichever contrasts more with c.
func (c rgb) foreground() rgb {
	white, ink := rgb{255, 255, 255}, rgb{0x1a, 0x1a, 0x1a}
	if contrast(c, white) >= contrast(c, ink) {
		return white
	}
	return ink
}

// themeRule renders one scheme's custom properties; the hover colour is darker in the
// light scheme and lighter in the dark one.
func themeRule(c rgb, dark bool) string {
	hover := c.mix(0, hoverShift)
	if dark {
		hover = c.mix(255, hoverShift)
	}
	return fmt.Sprintf("--primary: %s; --primary-fg: %s; --primary-hover: %s;", c.hex(), c.foreground().hex(), hover.hex())
}

// The media queries a theme rule sits in. The shipped palette (web/src/index.css) is a bare
// :root for light and the same :root inside prefers-color-scheme: dark, so a regional's
// colour must be guarded the same way or the later-loaded theme.css would beat the shipped
// dark tokens: the light colour sits under "not dark" (which also matches a browser with no
// preference) and the dark colour under dark, so an unset scheme keeps the shipped tokens.
const (
	mediaLight = "@media not (prefers-color-scheme: dark)"
	mediaDark  = "@media (prefers-color-scheme: dark)"
)

// renderTheme returns the stylesheet for the two brand colours ("" for none).
func renderTheme(light, dark string) string {
	var b strings.Builder
	if c, ok := parseHex(light); ok {
		b.WriteString(mediaLight + " { :root { " + themeRule(c, false) + " } }\n")
	}
	if c, ok := parseHex(dark); ok {
		b.WriteString(mediaDark + " { :root { " + themeRule(c, true) + " } }\n")
	}
	return b.String()
}

// safeField returns s cut and cleaned for use inside a field name that came from a client.
func safeField(s string) string {
	const limit = 64
	if len(s) > limit {
		s = s[:limit]
	}
	return strings.Map(func(r rune) rune {
		if r == utf8.RuneError || unicode.IsControl(r) {
			return '?'
		}
		return r
	}, s)
}
