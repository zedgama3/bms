package settings

import (
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
	regionalArea + "." + keyPublicURL:      func(v string) string { return strings.TrimRight(v, "/") },
	regionalArea + "." + keyColorPrimary:   strings.ToLower,
	regionalArea + "." + keyColorPrimaryDk: strings.ToLower,
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

func checkPublicURL(v string) error {
	if v == "" {
		return nil
	}
	if len(v) > MaxURLBytes {
		p := tooLong(MaxURLBytes)
		return fieldError("", p.code, p.params, p.reason)
	}
	bad := func(reason string) error {
		return fieldError("", codeURL, map[string]string{"schemes": "https, http"}, reason)
	}
	u, err := url.Parse(v)
	switch {
	case err != nil:
		return bad("must be a valid URL")
	case u.Scheme != "https" && u.Scheme != "http":
		return bad("must start with https:// or http://")
	case u.Hostname() == "":
		return bad("must name a host")
	case u.User != nil:
		return bad("must not contain a user name or password")
	case u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || strings.Contains(v, "#"):
		return bad("must not contain a query or a fragment")
	}
	return nil
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
