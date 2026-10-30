package intake

import (
	"fmt"
	"math"
	"regexp"
	"strings"
	"time"

	"github.com/zedgama3/bms/internal/core/telemetry"
	"github.com/zedgama3/bms/internal/lib/validate"
)

// Limits of the usage channel (telemetry T1.3). Constants, not settings, for the reason the
// error intake's are.
const (
	MaxEvents                 = 50      // events per batch
	EventsPerAddressPerMinute = 60      // events (not batches) per client address
	EventsGlobalPerMinute     = 2_000   // events per process
	MaxVitalMs                = 120_000 // lcp, inp and screen_ready values above this are a bug, not a measurement
	MaxCLS                    = 100
)

// EventKind is what the web shell measured.
type EventKind string

// The event kinds.
const (
	EventScreenView  EventKind = "screen_view"  // the router settled on a screen
	EventAction      EventKind = "action"       // a declared action on a screen (T1-c)
	EventScreenReady EventKind = "screen_ready" // navigation to first settled render, in ms
	EventVital       EventKind = "vital"        // a Web Vital's final value for the page load
)

// Event is one measurement. JSON field names are the wire form; unknown fields are ignored.
// Nothing in it may identify a person, a visit or a record: no IDs, no text, no URL.
type Event struct {
	Kind       EventKind `json:"kind"`
	Screen     string    `json:"screen"`                // the route pattern, "/volunteers/shifts/{id}"; required
	From       string    `json:"from,omitempty"`        // screen_view only: the previous pattern in this page load, or "_entry"
	Action     string    `json:"action,omitempty"`      // action only: a declared name, "shift.filter"
	Name       string    `json:"name,omitempty"`        // vital only: "lcp", "inp" or "cls"
	Value      float64   `json:"value,omitempty"`       // screen_ready and lcp, inp: milliseconds; cls: the score
	OccurredAt time.Time `json:"occurred_at"`           // RFC 3339; within OccurredWithin of now
	Count      int       `json:"count,omitempty"`       // screen_view and action: repeats merged by the shell; 0 means 1
	AppVersion string    `json:"app_version,omitempty"` // a Token of at most 64 bytes
}

// EventBatch is the request body of POST /api/client-events.
type EventBatch struct {
	Events []Event `json:"events"`
}

// The vital names.
const (
	vitalLCP = "lcp"
	vitalINP = "inp"
	vitalCLS = "cls"
)

var actionRE = regexp.MustCompile(`^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*){1,2}$`)

const maxActionBytes = 64

// Validate fails with errs.Invalid joining a validate.FieldError per problem, fields
// "events" and "events[i].<field>" (F54–F60, F62). It does not compare occurred_at with the
// clock (Usage.Accept does, F61).
func (b EventBatch) Validate() error {
	acc := validate.Errors{Op: "intake.EventBatch.Validate"}
	if n := len(b.Events); n < 1 || n > MaxEvents {
		acc.Add(validate.Range("events", n, 1, MaxEvents))
	}
	for i, e := range b.Events {
		prefix := fmt.Sprintf("events[%d].", i)
		e.check(func(field, code, reason string) { acc.Addf(prefix+field, code, "%s", reason) })
	}
	return acc.Err() //nolint:wrapcheck // Err already carries Op and errs.Invalid
}

// isScreenPattern reports whether s is a route pattern: a token beginning with "/".
func isScreenPattern(s string) bool { return strings.HasPrefix(s, "/") && telemetry.IsToken(s) }

// check reports every shape problem of e through add.
func (e Event) check(add func(field, code, reason string)) {
	switch e.Kind {
	case EventScreenView, EventAction, EventScreenReady, EventVital:
	default:
		add("kind", validate.CodeOneOf, "must be one of: screen_view, action, screen_ready, vital")
	}
	switch {
	case e.Screen == "":
		add("screen", validate.CodeRequired, "must be set")
	case !isScreenPattern(e.Screen):
		add("screen", codeMalformed, "must be a route pattern")
	}
	switch {
	case e.From == "":
	case e.Kind != EventScreenView:
		add("from", codeUnexpect, "must be absent unless kind is screen_view")
	case e.From != EntryToken && !isScreenPattern(e.From):
		add("from", codeMalformed, "must be a route pattern or _entry")
	}
	e.checkAction(add)
	e.checkValue(add)
	if e.Count < 0 || e.Count > MaxCount {
		add("count", validate.CodeRange, fmt.Sprintf("must be between 0 and %d", MaxCount))
	}
	if e.Count > 1 && (e.Kind == EventVital || e.Kind == EventScreenReady) {
		add("count", validate.CodeRange, "must be 0 or 1 for vital and screen_ready")
	}
	if e.AppVersion != "" && (len(e.AppVersion) > 64 || !telemetry.IsToken(e.AppVersion)) {
		add("app_version", codeMalformed, "must be a token of at most 64 bytes")
	}
}

func (e Event) checkAction(add func(field, code, reason string)) {
	switch {
	case e.Kind == EventAction && e.Action == "":
		add("action", validate.CodeRequired, "must be set when kind is action")
	case e.Kind != EventAction && e.Action != "":
		add("action", codeUnexpect, "must be absent unless kind is action")
	case e.Action != "" && (len(e.Action) > maxActionBytes || !actionRE.MatchString(e.Action)):
		add("action", codeMalformed, "must be a declared action name such as shift.filter")
	}
}

func (e Event) checkValue(add func(field, code, reason string)) {
	if math.IsNaN(e.Value) || math.IsInf(e.Value, 0) {
		add("value", validate.CodeRange, "must be a finite number")
		return
	}
	switch e.Kind {
	case EventVital:
		switch e.Name {
		case vitalLCP, vitalINP:
			if e.Value < 0 || e.Value > MaxVitalMs {
				add("value", validate.CodeRange, fmt.Sprintf("must be between 0 and %d", MaxVitalMs))
			}
		case vitalCLS:
			if e.Value < 0 || e.Value > MaxCLS {
				add("value", validate.CodeRange, fmt.Sprintf("must be between 0 and %d", MaxCLS))
			}
		default:
			add("name", validate.CodeOneOf, "must be one of: lcp, inp, cls")
		}
	case EventScreenReady:
		if e.Name != "" {
			add("name", codeUnexpect, "must be absent unless kind is vital")
		}
		if e.Value <= 0 || e.Value > MaxVitalMs {
			add("value", validate.CodeRange, fmt.Sprintf("must be greater than 0 and at most %d", MaxVitalMs))
		}
	case EventScreenView, EventAction:
		if e.Name != "" {
			add("name", codeUnexpect, "must be absent unless kind is vital")
		}
		if e.Value != 0 {
			add("value", codeUnexpect, "must be absent for screen_view and action")
		}
	}
}
