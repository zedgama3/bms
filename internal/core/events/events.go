package events

import (
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/zedgama3/bms/internal/core/audit"
	"github.com/zedgama3/bms/internal/core/authz"
	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/events/postgres"
	"github.com/zedgama3/bms/internal/core/events/rows"
	"github.com/zedgama3/bms/internal/core/signals"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/validate"
	"github.com/zedgama3/bms/internal/model"
)

// Limits and bounds.
const (
	MaxNameBytes        = 100
	MaxKindKeyBytes     = 32
	MaxKindNameBytes    = 60
	MaxLabelBytes       = 100
	MaxZoneBytes        = 64
	MaxScheduleItems    = 100
	MaxEventDuration    = 366 * 24 * time.Hour
	MaxInfo             = 200
	DefaultSetupTimeout = 10 * time.Second

	// minSetupTimeout and maxSetupTimeout bound Options.SetupTimeout.
	minSetupTimeout = time.Second
	maxSetupTimeout = 5 * time.Minute
)

// PermissionManage is the one permission events declares; app adds Permissions to
// corePerms.
const PermissionManage authz.Permission = "events.manage" // create, change, close, reopen events; kinds; schedule; RunSetup

// Permissions lists the declaration of events.manage with its starter role
// (departments-events.md, "Permissions").
var Permissions = []authz.PermissionDecl{
	{
		Name: PermissionManage, Scope: authz.ScopeSite, Reaches: model.SensitivityInternal,
		StarterRoles: []string{authz.RoleEventAdmin},
	},
}

// The signals events publishes; the payload carries IDs only.
const (
	SignalCreated  signals.Name = "events.event-created"
	SignalChanged  signals.Name = "events.event-changed"
	SignalClosed   signals.Name = "events.event-closed"
	SignalReopened signals.Name = "events.event-reopened"

	PayloadEvent      = "event"
	PayloadCopiedFrom = "copied-from"
)

// PublishedSignals lists the signals events publishes, for app's scope and
// Env.CoreSignals. Callers must not modify it.
var PublishedSignals = []signals.Name{SignalCreated, SignalChanged, SignalClosed, SignalReopened}

// The audit actions events records, and the record kinds they point at.
const (
	actionCreated      audit.Action = "events.event.created"
	actionChanged      audit.Action = "events.event.changed"
	actionClosed       audit.Action = "events.event.closed"
	actionReopened     audit.Action = "events.event.reopened"
	actionKindCreated  audit.Action = "events.kind.created"
	actionKindChanged  audit.Action = "events.kind.changed"
	actionItemCreated  audit.Action = "events.schedule-item.created"
	actionItemChanged  audit.Action = "events.schedule-item.changed"
	actionItemDeleted  audit.Action = "events.schedule-item.deleted"
	actionSetupRan     audit.Action = "events.setup.ran"
	kindEvent                       = "events.event"
	kindKind                        = "events.kind"
	kindScheduleItem                = "events.schedule-item"
	reasonCopyFromHead              = "copy_from:"
)

// Set is the migration set app adds to coreSets.
var Set = db.Set{Name: "events", Schema: "core", Files: postgres.Migrations}

// The seeded kinds (migration 0001 inserts them, flagged Builtin).
const (
	KindBurn          = "burn"
	KindDecompression = "decompression"
	KindOther         = "other"
)

// Field names, as the audit entries and the API name them.
const (
	fieldName   = "name"
	fieldKind   = "kind"
	fieldDates  = "dates"
	fieldZone   = "zone"
	fieldKey    = "key"
	fieldLabel  = "label"
	fieldStarts = "starts_at"
	fieldEnds   = "ends_at"
)

// Field codes events adds to the shell's vocabulary.
const (
	codeRequired  = validate.CodeRequired
	codeMalformed = validate.CodeMalformed
	codeOneOf     = validate.CodeOneOf
	codeRange     = validate.CodeRange
	codeTooLong   = "too_long"      // params: max
	codeTooMany   = "too_many"      // params: max
	codeControl   = "control_chars" // a control character in a text field
	codeUnknown   = "unknown"       // a kind or an event that does not exist
	codeSelf      = "self"          // copy_from names the event itself
	codeTaken     = "taken"         // a name or kind key in use
	codeClosed    = "closed"        // the event is closed
	codeOpen      = "open"          // the event is open
	codeDuplicate = "duplicate"     // a schedule key in use in this event
)

// keyRE is the ident grammar of kind and schedule keys.
var keyRE = regexp.MustCompile(`^[a-z][a-z0-9]*(-[a-z0-9]+)*$`)

// Kind is a named kind of event. The three seeded kinds are Builtin: the UI shows them
// through catalogue keys core.events.kind.<key>; custom kinds show Name.
type Kind struct {
	Key       string    `json:"key"`
	Name      string    `json:"name"`
	Builtin   bool      `json:"builtin"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// KindInput creates or renames a kind. Key is immutable after creation (ident grammar, at
// most MaxKindKeyBytes); UpdateKind ignores it.
type KindInput struct {
	Key  string `json:"key"`
	Name string `json:"name"`
}

// Validate fails errs.Invalid joining a validate.FieldError per problem, on key (required,
// malformed, too_long) and name (required, too_long, control_chars, malformed). Messages
// never echo a value.
func (in KindInput) Validate() error {
	v := validate.Errors{Op: "events.KindInput.Validate"}
	checkKey(&v, fieldKey, in.Key, MaxKindKeyBytes, true)
	checkLabel(&v, fieldName, in.Name, MaxKindNameBytes)
	return v.Err() //nolint:wrapcheck // Err already carries Op and errs.Invalid
}

// Event is a gathering the regional puts on. Dates are instants; Zone is the IANA name the
// dates are shown in.
type Event struct {
	ID        model.EventID   `json:"id"`
	Name      string          `json:"name"`
	Kind      string          `json:"kind"`
	Dates     model.TimeRange `json:"dates"`
	Zone      string          `json:"zone"`
	Closed    bool            `json:"closed"`
	ClosedAt  time.Time       `json:"closed_at,omitzero"`
	CreatedAt time.Time       `json:"created_at"`
	UpdatedAt time.Time       `json:"updated_at"`
}

// Validate fails errs.Invalid joining a validate.FieldError per problem on name, kind
// (shape only), dates.start, dates.end (required; range for an end not after the start or
// a duration over MaxEventDuration) and zone (required, too_long, malformed). Messages
// never echo a value.
func (e Event) Validate() error {
	v := validate.Errors{Op: "events.Event.Validate"}
	checkEventName(&v, e.Name)
	checkKindShape(&v, e.Kind)
	checkDates(&v, e.Dates)
	checkZone(&v, e.Zone)
	return v.Err() //nolint:wrapcheck // Err already carries Op and errs.Invalid
}

// LogValue emits the ID only, so slog never prints an event's name. Cannot fail.
func (e Event) LogValue() slog.Value {
	return slog.GroupValue(slog.String("event_id", e.ID.String()))
}

// ScheduleItemID identifies a schedule item.
type ScheduleItemID = rows.ScheduleItemID

// ScheduleItem is a window or a moment inside an event (gate hours, the temple burn). Key
// is an optional token unique within the event ("temple-burn"), for modules that anchor to
// it later; Ends zero means a moment.
type ScheduleItem struct {
	ID       ScheduleItemID `json:"id"`
	Event    model.EventID  `json:"event"`
	Key      string         `json:"key,omitempty"`
	Label    string         `json:"label"`
	Starts   time.Time      `json:"starts_at"`
	Ends     time.Time      `json:"ends_at,omitzero"`
	Position int            `json:"position"`
}

// Detail is an event with its schedule, sorted by Starts then Position.
type Detail struct {
	Event
	Schedule []ScheduleItem `json:"schedule"`
}

// CreateInput creates an event. CopyFrom (optional) names the event whose module data the
// events.event-setup providers copy.
type CreateInput struct {
	Name     string          `json:"name"`
	Kind     string          `json:"kind"`
	Dates    model.TimeRange `json:"dates"`
	Zone     string          `json:"zone"`
	CopyFrom model.EventID   `json:"copy_from,omitzero"`
}

// UpdateInput changes an event; nil means unchanged.
type UpdateInput struct {
	Name  *string          `json:"name"`
	Kind  *string          `json:"kind"`
	Zone  *string          `json:"zone"`
	Dates *model.TimeRange `json:"dates"`
}

// ScheduleInput adds or replaces a schedule item.
type ScheduleInput struct {
	Key    string    `json:"key"`
	Label  string    `json:"label"`
	Starts time.Time `json:"starts_at"`
	Ends   time.Time `json:"ends_at,omitzero"`
}

// Validate fails errs.Invalid joining a validate.FieldError per problem on key
// (malformed, too_long), label (required, too_long, control_chars, malformed), starts_at
// (required) and ends_at (range: not after starts_at). Messages never echo a value.
func (in ScheduleInput) Validate() error {
	v := validate.Errors{Op: "events.ScheduleInput.Validate"}
	checkKey(&v, fieldKey, in.Key, MaxKindKeyBytes, false)
	checkLabel(&v, fieldLabel, in.Label, MaxLabelBytes)
	if in.Starts.IsZero() {
		v.Addf(fieldStarts, codeRequired, "must be set")
	}
	if !in.Ends.IsZero() && !in.Starts.IsZero() && !in.Ends.After(in.Starts) {
		v.Add(errs.E("", errs.Invalid, &validate.FieldError{
			Field: fieldEnds, Code: codeRange, Reason: "must be after starts_at",
		}))
	}
	return v.Err() //nolint:wrapcheck // Err already carries Op and errs.Invalid
}

// Status filters List.
type Status uint8

// The statuses. The zero value is not a status; ListQuery treats it as StatusAny.
const (
	StatusAny Status = iota + 1
	StatusOpen
	StatusClosed
)

var statusWords = [...]string{StatusAny: "any", StatusOpen: "open", StatusClosed: "closed"}

func (s Status) valid() bool { return s >= StatusAny && s <= StatusClosed }

// String returns "any", "open", "closed" or "Status(<n>)". Cannot fail.
func (s Status) String() string {
	if s.valid() {
		return statusWords[s]
	}
	return fmt.Sprintf("Status(%d)", uint8(s))
}

// ParseStatus returns the status spelled "any", "open" or "closed". Fails errs.Invalid
// (status, one_of); the error never echoes s.
func ParseStatus(s string) (Status, error) {
	for st := StatusAny; st <= StatusClosed; st++ {
		if statusWords[st] == s {
			return st, nil
		}
	}
	return 0, fieldInvalid("events.ParseStatus", "status", codeOneOf, "must be one of: any, open, closed")
}

// ListQuery filters List.
type ListQuery struct {
	Status Status // zero = StatusAny
}

// SetupResult is one provider's outcome. Code is the errs kind name when Failed
// ("permission", "timeout", "unavailable", ...), else "".
type SetupResult struct {
	Module string `json:"module"`
	Items  int    `json:"items"`
	Failed bool   `json:"failed"`
	Code   string `json:"code,omitempty"`
}

// Created is Create's answer: the event and what each provider did. Setup is empty, never
// nil, when nothing ran.
type Created struct {
	Event Event         `json:"event"`
	Setup []SetupResult `json:"setup"`
}

// Info is what modules get about an event.
type Info struct {
	ID     model.EventID
	Name   string
	Kind   string
	Dates  model.TimeRange
	Zone   string
	Closed bool
}

// ---- conversions ------------------------------------------------------------------------

func eventOf(r rows.Event) Event {
	return Event{
		ID: r.ID, Name: r.Name, Kind: r.Kind, Dates: model.TimeRange{Start: r.Starts, End: r.Ends}, Zone: r.Zone,
		Closed: !r.ClosedAt.IsZero(), ClosedAt: r.ClosedAt, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
	}
}

func kindOf(r rows.Kind) Kind {
	return Kind{Key: r.Key, Name: r.Name, Builtin: r.Builtin, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt}
}

func itemOf(r rows.ScheduleItem) ScheduleItem {
	return ScheduleItem{ID: r.ID, Event: r.Event, Key: r.Key, Label: r.Label, Starts: r.Starts, Ends: r.Ends, Position: r.Position}
}

func infoOf(r rows.Info) Info {
	return Info{ID: r.ID, Name: r.Name, Kind: r.Kind, Dates: model.TimeRange{Start: r.Starts, End: r.Ends}, Zone: r.Zone, Closed: r.Closed}
}

// ---- validation -------------------------------------------------------------------------

func addTooLong(v *validate.Errors, field string, limit int) {
	v.Add(errs.E("", errs.Invalid, &validate.FieldError{
		Field: field, Code: codeTooLong, Params: map[string]string{"max": fmt.Sprint(limit)},
		Reason: fmt.Sprintf("must be at most %d bytes", limit),
	}))
}

// fieldInvalid builds an errs.Invalid error carrying one validate.FieldError.
func fieldInvalid(op, field, code, format string, args ...any) error {
	v := validate.Errors{Op: op}
	v.Addf(field, code, format, args...)
	return v.Err() //nolint:wrapcheck // Err already carries Op and errs.Invalid
}

// paramInvalid is fieldInvalid with params.
func paramInvalid(op, field, code string, params map[string]string, reason string) error {
	return errs.E(op, errs.Invalid, &validate.FieldError{Field: field, Code: code, Params: params, Reason: reason})
}

// fieldConflict builds an errs.Conflict error carrying one validate.FieldError (the api
// shows the fields of a Conflict as it does those of an Invalid).
func fieldConflict(op, field, code, reason string) error {
	return errs.E(op, errs.Conflict, &validate.FieldError{Field: field, Code: code, Reason: reason})
}

// checkText checks s as it is: valid UTF-8, no control characters, at most limit bytes.
func checkText(v *validate.Errors, field, s string, limit int) {
	switch {
	case !utf8.ValidString(s):
		v.Addf(field, codeMalformed, "must be valid UTF-8")
	case strings.IndexFunc(s, unicode.IsControl) >= 0:
		v.Addf(field, codeControl, "must not contain control characters")
	case len(s) > limit:
		addTooLong(v, field, limit)
	}
}

// checkLabel checks a required one-line text.
func checkLabel(v *validate.Errors, field, s string, limit int) {
	if s == "" {
		v.Addf(field, codeRequired, "must not be empty")
		return
	}
	checkText(v, field, s, limit)
}

func checkEventName(v *validate.Errors, s string) { checkLabel(v, fieldName, s, MaxNameBytes) }

// checkKey checks an ident key; an empty one is an error only when required.
func checkKey(v *validate.Errors, field, s string, limit int, required bool) {
	switch {
	case s == "":
		if required {
			v.Addf(field, codeRequired, "must not be empty")
		}
	case len(s) > limit:
		addTooLong(v, field, limit)
	case !keyRE.MatchString(s):
		v.Addf(field, codeMalformed, "must be lower-case letters, digits and single hyphens, starting with a letter")
	}
}

// checkKindShape checks the shape of an event's kind (that it exists is the store's).
func checkKindShape(v *validate.Errors, s string) {
	checkKey(v, fieldKind, s, MaxKindKeyBytes, true)
}

// checkZone requires a name time.LoadLocation knows; "" and "Local" are not zones.
func checkZone(v *validate.Errors, s string) {
	switch {
	case s == "":
		v.Addf(fieldZone, codeRequired, "must not be empty")
	case len(s) > MaxZoneBytes:
		addTooLong(v, fieldZone, MaxZoneBytes)
	case s == "Local":
		v.Addf(fieldZone, codeMalformed, "must be an IANA zone name")
	default:
		if _, err := time.LoadLocation(s); err != nil {
			v.Addf(fieldZone, codeMalformed, "must be an IANA zone name")
		}
	}
}

// checkDates runs model.TimeRange.Validate under the prefix "dates." (an end not after the
// start is code range) and adds the duration bound.
func checkDates(v *validate.Errors, r model.TimeRange) {
	for _, fe := range validate.Fields(r.Validate()) {
		code := fe.Code
		if code != codeRequired {
			code = codeRange
		}
		v.Add(errs.E("", errs.Invalid, &validate.FieldError{
			Field: fieldDates + "." + fe.Field, Code: code, Params: fe.Params, Reason: fe.Reason,
		}))
	}
	if !r.Start.IsZero() && !r.End.IsZero() && r.Start.Before(r.End) && r.Duration() > MaxEventDuration {
		v.Add(errs.E("", errs.Invalid, &validate.FieldError{
			Field: fieldDates + ".end", Code: codeRange,
			Params: map[string]string{"max": MaxEventDuration.String()},
			Reason: "the event may last at most 366 days",
		}))
	}
}
