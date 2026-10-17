package telemetry

import (
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/zedgama3/bms/internal/lib/errs"
)

// MaxTextBytes caps a Text attribute and exception.message. MaxLogValueBytes caps any
// string value in a log record (a stack trace keeps its top frames). A capped string is
// cut at a character boundary and ends with "…", and the whole result fits in the cap.
// MaxStackLogBytes is the larger cap for the one key that carries a stack trace,
// client.stack (see stackKey); intake.MaxStackBytes is defined as this value.
const (
	MaxTextBytes     = 512
	MaxLogValueBytes = 2048

	MaxStackLogBytes = 4096
	stackKey         = "client.stack"
)

// logValueCap is the byte cap for a string log value under key.
func logValueCap(key string) int {
	if key == stackKey {
		return MaxStackLogBytes
	}
	return MaxLogValueBytes
}

// Markers that replace a value the policy refused.
const (
	markerDenied       = "[denied]"
	markerInvalidToken = "[invalid-token]"
	markerEmail        = "[email]"
	markerQuery        = "?[query]"
	ellipsis           = "…"
	invalidSpanName    = "invalid-span-name"
	invalidKeyAttr     = "telemetry.invalid_key"
)

// Violation signals and rules (see Violation).
const (
	signalLog    = "log"
	signalTrace  = "trace"
	signalMetric = "metric"

	ruleDeniedKey        = "denied_key"
	ruleInvalidKey       = "invalid_key"
	ruleInvalidToken     = "invalid_token"
	ruleInvalidSpanName  = "invalid_span_name"
	ruleTextOnMetric     = "text_on_metric"
	ruleIDOnMetric       = "id_on_metric"
	ruleNegativeCount    = "negative_count"
	ruleInvalidMetricVal = "invalid_metric_attr"
)

// maxKeyBytes caps the Key of a Violation and the key echoed in telemetry.invalid_key.
const maxKeyBytes = 64

// Violation is one breach of the policy that was corrected at the point of emission.
type Violation struct {
	Signal string // "log", "trace", "metric", "client"
	Rule   string // "denied_key", "invalid_key", "invalid_token", "invalid_span_name", "text_on_metric", "id_on_metric", "negative_count"
	Key    string // the attribute key or span name involved, capped at 64 bytes
}

// attrKind is the value kind of an Attr; metrics accept only some of them.
type attrKind uint8

const (
	kindNone attrKind = iota
	kindInt
	kindInt64
	kindFloat
	kindBool
	kindDuration
	kindToken
	kindText
	kindID
)

// Attr is one key-value pair for a span, a metric or a log record. Build it with the
// constructors; each applies the policy, so a Span or Counter never sees a raw value. The
// zero Attr is ignored.
type Attr struct {
	key  string
	kind attrKind
	s    string
	n    int64
	f    float64
	// viol is the rule the constructor broke, if any. The consumer (Span, Counter, ...)
	// reports it, because a constructor has no Telemetry to report to.
	viol string
}

// Key returns the attribute's key after policy. Cannot fail. For tests.
func (a Attr) Key() string { return a.key }

func (a Attr) isZero() bool { return a.key == "" }

// violKey is the key to name in a Violation: the offending key for an invalid one (already
// scrubbed and capped, it is held as the value), else the Attr's own key.
func (a Attr) violKey() string {
	if a.viol == ruleInvalidKey {
		return a.s
	}
	return a.key
}

// text renders the value as text, for observers.
func (a Attr) text() string {
	switch a.kind {
	case kindInt, kindInt64, kindDuration:
		return strconv.FormatInt(a.n, 10)
	case kindFloat:
		return strconv.FormatFloat(a.f, 'g', -1, 64)
	case kindBool:
		return strconv.FormatBool(a.n != 0)
	case kindToken, kindText, kindID:
		return a.s
	default:
		return ""
	}
}

// keyOK reports whether k matches [a-z][a-z0-9_.]{0,63}.
func keyOK(k string) bool {
	if k == "" || len(k) > 64 {
		return false
	}
	if k[0] < 'a' || k[0] > 'z' {
		return false
	}
	for i := 1; i < len(k); i++ {
		c := k[i]
		if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '_' && c != '.' {
			return false
		}
	}
	return true
}

// deniedKeys is the key denylist (Behavior: "The key denylist"). A key is normalised
// (lower-cased, "." and "-" replaced by "_") and denied when it equals an entry or ends
// with "_" followed by one.
var deniedKeys = []string{
	"email", "e_mail", "legal_name", "first_name", "last_name", "full_name", "display_name",
	"playa_name", "phone", "tel", "mobile", "address", "street", "emergency_contact",
	"password", "passwd", "secret", "token", "authorization", "cookie", "set_cookie",
	"session", "body", "request_body", "response_body", "query", "query_string", "url_full",
	"url_query", "sql", "statement", "args", "params", "form", "narrative", "notes",
	"medical", "diagnosis",
}

var deniedSet = func() map[string]struct{} {
	m := make(map[string]struct{}, len(deniedKeys))
	for _, k := range deniedKeys {
		m[k] = struct{}{}
	}
	return m
}()

var keyNormaliser = strings.NewReplacer(".", "_", "-", "_")

// keyDenied reports whether key is on the denylist.
func keyDenied(key string) bool {
	n := keyNormaliser.Replace(strings.ToLower(key))
	if _, ok := deniedSet[n]; ok {
		return true
	}
	// Every entry is checked as a "_"-delimited suffix: walk the "_" positions.
	for i := 0; i < len(n); i++ {
		if n[i] != '_' {
			continue
		}
		if _, ok := deniedSet[n[i+1:]]; ok {
			return true
		}
	}
	return false
}

// base builds an Attr after the key rules (F15, F16). ok is false when the value must be
// replaced by the marker (denied key); in that case the returned Attr already holds it.
func base(key string) (a Attr, replaced bool) {
	if !keyOK(key) {
		return Attr{
			key:  invalidKeyAttr,
			kind: kindText,
			s:    capKey(key),
			viol: ruleInvalidKey,
		}, true
	}
	if keyDenied(key) {
		return Attr{key: key, kind: kindToken, s: markerDenied, viol: ruleDeniedKey}, true
	}
	return Attr{key: key}, false
}

// capKey scrubs and caps a key echoed in a marker or violation.
func capKey(k string) string {
	return capBytes(Scrub(k), maxKeyBytes)
}

// Int records an int.
func Int(key string, v int) Attr { return Int64(key, int64(v)).withKind(kindInt) }

// Int64 records an int64.
func Int64(key string, v int64) Attr {
	a, replaced := base(key)
	if replaced {
		return a
	}
	a.kind, a.n = kindInt64, v
	return a
}

// Float records a float64.
func Float(key string, v float64) Attr {
	a, replaced := base(key)
	if replaced {
		return a
	}
	a.kind, a.f = kindFloat, v
	return a
}

// Bool records a boolean.
func Bool(key string, v bool) Attr {
	a, replaced := base(key)
	if replaced {
		return a
	}
	a.kind = kindBool
	if v {
		a.n = 1
	}
	return a
}

// Duration records d in milliseconds as an int64; by convention the key ends in "_ms".
func Duration(key string, d time.Duration) Attr {
	a, replaced := base(key)
	if replaced {
		return a
	}
	a.kind, a.n = kindDuration, d.Milliseconds()
	return a
}

func (a Attr) withKind(k attrKind) Attr {
	if a.viol == "" {
		a.kind = k
	}
	return a
}

// IsToken reports whether s fits the token grammar
// "[A-Za-z0-9/_][A-Za-z0-9._:/{}-]{0,127}" (no space, no "@", no "?"). It is what Token
// and Module accept; intake uses it to check route patterns a client sends.
func IsToken(s string) bool {
	if s == "" || len(s) > 128 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '/', c == '_':
		case i > 0 && (c == '.' || c == ':' || c == '{' || c == '}' || c == '-'):
		default:
			return false
		}
	}
	return true
}

// Token records a short identifier-like string: a route pattern, an HTTP method, a module
// or signal name, an operation, an SQL keyword, a status word. A value that fails the
// grammar (IsToken) yields "[invalid-token]" and a Violation when the Attr is used (F17).
// Tokens are the only strings allowed on metrics.
func Token(key, value string) Attr {
	a, replaced := base(key)
	if replaced {
		return a
	}
	a.kind = kindToken
	if !IsToken(value) {
		a.s, a.viol = markerInvalidToken, ruleInvalidToken
		return a
	}
	a.s = value
	return a
}

// Text records free text: an error message, a client's crash message. The value goes
// through Scrub and is cut at MaxTextBytes with "…" appended (F18). Text is never allowed
// on a metric (F19). Use it last; prefer Token or an ID.
func Text(key, value string) Attr {
	a, replaced := base(key)
	if replaced {
		return a
	}
	a.kind = kindText
	a.s = capBytes(Scrub(value), MaxTextBytes)
	return a
}

// ID records a record identifier in canonical UUID form. A zero id yields the zero Attr,
// which is ignored. IDs are never allowed on a metric (F19).
func ID[T ~[16]byte](key string, id T) Attr {
	b := [16]byte(id)
	if b == ([16]byte{}) {
		return Attr{}
	}
	a, replaced := base(key)
	if replaced {
		return a
	}
	a.kind, a.s = kindID, formatUUID(b)
	return a
}

// Kind records k.String() under "error.kind". Cannot fail.
func Kind(k errs.Kind) Attr {
	return Attr{key: "error.kind", kind: kindToken, s: k.String()}
}

// Module records a module name under "module". Same grammar as Token.
func Module(name string) Attr { return Token("module", name) }

const hexDigits = "0123456789abcdef"

func formatUUID(b [16]byte) string {
	var buf [36]byte
	j := 0
	for i, c := range b {
		if i == 4 || i == 6 || i == 8 || i == 10 {
			buf[j] = '-'
			j++
		}
		buf[j] = hexDigits[c>>4]
		buf[j+1] = hexDigits[c&0x0f]
		j += 2
	}
	return string(buf[:])
}

// ---- Scrub and caps ----

var (
	emailRE = regexp.MustCompile(`[^\s@<>,;"]+@[^\s@<>,;"]+\.[^\s@<>,;"./]+`)
	wordRE  = regexp.MustCompile(`\S+`)
)

// Scrub returns s with every e-mail-shaped token replaced by "[email]" and every "?query"
// part of a URL-shaped token replaced by "?[query]". It is applied to every string that
// reaches a log, a span or a client report. It does not cap length (the caller does) and
// does not detect names or phone numbers, which have no reliable shape: those are kept
// out by using IDs (go-conventions) and by the key denylist. Cannot fail; pure.
func Scrub(s string) string {
	if strings.IndexByte(s, '@') >= 0 {
		s = emailRE.ReplaceAllString(s, markerEmail)
	}
	if strings.IndexByte(s, '?') >= 0 {
		s = wordRE.ReplaceAllStringFunc(s, func(w string) string {
			if !strings.Contains(w, "://") && w[0] != '/' {
				return w
			}
			i := strings.IndexByte(w, '?')
			if i < 0 {
				return w
			}
			return w[:i] + markerQuery
		})
	}
	return s
}

// capBytes returns s unchanged when it fits in limit bytes, otherwise a valid-UTF-8 prefix
// followed by "…" whose total length is at most limit.
func capBytes(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	cut := limit - len(ellipsis)
	if cut < 0 {
		cut = 0
	}
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + ellipsis
}

// ---- Span names ----

// spanNameOK reports whether name is a token, or "<METHOD> <route pattern>" (decision 19):
// an upper-case method ([A-Z]{3,10}), exactly one space, then a token beginning with "/",
// the whole at most 128 bytes.
func spanNameOK(name string) bool {
	if len(name) > 128 {
		return false
	}
	if IsToken(name) {
		return true
	}
	sp := strings.IndexByte(name, ' ')
	if sp < 3 || sp > 10 {
		return false
	}
	for i := 0; i < sp; i++ {
		if name[i] < 'A' || name[i] > 'Z' {
			return false
		}
	}
	route := name[sp+1:]
	return route != "" && route[0] == '/' && IsToken(route)
}
