package datarights

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/zedgama3/bms/internal/core/audit"
	"github.com/zedgama3/bms/internal/core/authz"
	"github.com/zedgama3/bms/internal/core/datarights/rows"
	"github.com/zedgama3/bms/internal/core/files"
	"github.com/zedgama3/bms/internal/core/people"
	"github.com/zedgama3/bms/internal/core/signals"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/validate"
	"github.com/zedgama3/bms/internal/model"
)

// Area is the audit scope, signal scope, notify owner, route set and span prefix of this
// package: the name a core area carries (module.IsCoreArea).
const Area = "datarights"

// Limits and bounds.
const (
	// ExportCooldown is how long a person must wait between exports of their own data.
	ExportCooldown = 10 * time.Minute
	// MaxTrackedExports bounds the cooldown map (the oldest entry is evicted past it).
	MaxTrackedExports = 10000
	// MaxNoteBytes is the longest request or decision note.
	MaxNoteBytes = 500

	// ExportFormat is the "format" of the export document.
	ExportFormat = 1

	// maxRequestsListed bounds MyRequests (a person's requests are finite: one open at a
	// time, the rest decided).
	maxRequestsListed = 200
)

// The permissions datarights declares. The names are people's (boundaries.md decision 12:
// a rename with a data migration may come later), so people keeps the constants it needs
// for its own page's capabilities and these are the same values.
const (
	PermissionDataExport = people.PermissionDataExport // export everything about any person
	PermissionDataDelete = people.PermissionDataDelete // delete a person; decide deletion requests
)

// Permissions lists the declarations of the two permissions above, with their starter
// roles (people.md, "Permissions"). Callers must not modify it.
var Permissions = []authz.PermissionDecl{
	{
		Name: PermissionDataExport, Scope: authz.ScopeSite, Reaches: model.SensitivitySensitive,
		StarterRoles: []string{authz.RoleAccountAdmin},
	},
	{
		Name: PermissionDataDelete, Scope: authz.ScopeSite, Reaches: model.SensitivitySensitive,
		StarterRoles: []string{authz.RoleAccountAdmin},
	},
}

// The signal datarights publishes, with its payload keys.
const (
	// SignalDeletionRequested is published when a person files a deletion request:
	// {request, person}. It replaces people.deletion-requested (signals' owner rule).
	SignalDeletionRequested signals.Name = "datarights.deletion-requested"

	PayloadPerson  = "person"
	PayloadRequest = "request"
)

// PublishedSignals lists the signals datarights publishes, for app's scope and
// Env.CoreSignals. It subscribes to none. Callers must not modify it.
var PublishedSignals = []signals.Name{SignalDeletionRequested}

// The audit actions datarights records, and the record kinds they point at. Entries before
// boundaries step 12 keep the names people.person.exported, people.person.deleted and
// people.request.*.
const (
	actionExport           audit.Action = "datarights.export"
	actionDelete           audit.Action = "datarights.delete"
	actionRequestCreated   audit.Action = "datarights.request.created"
	actionRequestRead      audit.Action = "datarights.request.read"
	actionRequestWithdraw  audit.Action = "datarights.request.withdrawn"
	actionRequestDeclined  audit.Action = "datarights.request.declined"
	actionRequestFulfilled audit.Action = "datarights.request.fulfilled"

	kindPerson  = "people.person"
	kindRequest = "people.request"
)

// The reasons recorded on the export and deletion entries: tokens, never free text.
const (
	reasonAdmin   = "admin"
	reasonCommand = "command"
)

// Field codes datarights adds to the shell's vocabulary.
const (
	codeRequired  = validate.CodeRequired
	codeMalformed = validate.CodeMalformed
	codeOneOf     = validate.CodeOneOf
	codeTooLong   = "too_long" // params: max
	codeControl   = "control_chars"
	codeSelf      = "self"      // an action aimed at oneself that must not be
	codeDuplicate = "duplicate" // an open request exists
	codeStatus    = "status"    // the request is not open
	codeInUse     = "in_use"    // a foreign key still references the person
	codeProtected = "protected" // identity's Guard refuses: a shared demo account
)

// auditEntryKind is the Kept kind of the audit rows deletion leaves (audit decision 10).
const auditEntryKind = "audit.entry"

// ---- the types module-contract aliases --------------------------------------------------

// Export is one source's share of a person's data export.
type Export struct {
	Data json.RawMessage // a JSON object, or empty when the source holds nothing
}

// Kept is one kind of record a source keeps on deletion; the reason is documented in the
// source's spec and shown through the message key module.<name>.kept.<kind>.
type Kept struct {
	Kind  string // one ident
	Count int
}

// Deletion reports what one source's DeletePerson did.
type Deletion struct {
	Deleted    int    // records removed
	Anonymized int    // records kept with personal fields irreversibly replaced
	Kept       []Kept // records kept as they are, by the source's spec
}

// PersonData is how a module answers "export everything about person X" and "delete
// everything about person X" (privacy-data). module.PersonData is an alias of it. See
// module.PersonData for the contract a module implements.
type PersonData interface {
	// ExportPerson returns every record held about person, at every sensitivity level, as
	// one JSON object. An empty Data means nothing is held. Fails errs.Invalid for a zero
	// person and with the store's kind otherwise.
	ExportPerson(ctx context.Context, person model.PersonID) (Export, error)
	// DeletePerson deletes or irreversibly anonymizes every record held about person,
	// including attachments, and reports what it did. Idempotent. Fails errs.Invalid for a
	// zero person; on a read-only copy, errs.Unavailable wrapping db.ErrReadOnly.
	DeletePerson(ctx context.Context, person model.PersonID) (Deletion, error)
}

// Source is one module's PersonData with its name, in build order.
type Source struct {
	Name string
	PersonData
}

// ---- the core sources, in the shapes the services already have --------------------------

// RawSource is a core service that exports a JSON document and deletes counted rows:
// identity, authz and notify. DeletePerson runs inside the caller's Write transaction.
type RawSource interface {
	ExportPerson(ctx context.Context, person model.PersonID) (json.RawMessage, error)
	DeletePerson(ctx context.Context, person model.PersonID) (int, error)
}

// AccountSource is identity: a RawSource that also says whether the person has an account.
// EmailOf fails errs.NotFound when the person has none; datarights asks it for a person who
// has no profile row, whose identity, access and module data still need exporting or
// deleting (people.md D37).
type AccountSource interface {
	RawSource
	EmailOf(ctx context.Context, person model.PersonID) (model.Email, error)
}

// FilesSource is files.Service: its export document and its backstop deletion, which
// reports files.Deletion.
type FilesSource interface {
	ExportPerson(ctx context.Context, person model.PersonID) (json.RawMessage, error)
	DeletePerson(ctx context.Context, person model.PersonID) (files.Deletion, error)
}

// PeopleSource is people.Service: its own rows, last in the deletion order.
type PeopleSource interface {
	// ExportPerson returns {"person": {...}, "emergency_contacts": [...], "consents":
	// [...]} for the person; errs.NotFound when there is no profile row.
	ExportPerson(ctx context.Context, person model.PersonID) (json.RawMessage, error)
	// DeletePerson deletes the person row, contacts and consents inside the caller's Write
	// transaction and publishes people.person-deleted after commit; errs.Conflict when a
	// module still references the person (FK RESTRICT: a programming error in a module).
	DeletePerson(ctx context.Context, person model.PersonID) (int, error)
	// Exists reports whether the person row exists (errs.NotFound otherwise).
	Exists(ctx context.Context, person model.PersonID) error
}

// AuditLog is auditlog.Service's CountAbout, for the deletion report: the audit rows that
// name the person stay and are counted.
type AuditLog interface {
	CountAbout(ctx context.Context, person model.PersonID) (int, error)
}

// HolderSource is authz's seam for "who is appointed to this job": the authz.Lookup. It
// returns the persons who hold the permission site-wide now through an explicit grant
// (never the base role), superusers excluded.
type HolderSource interface {
	HoldersOf(ctx context.Context, p authz.Permission) ([]model.PersonID, error)
}

// ---- requests and the report ------------------------------------------------------------

// RequestID identifies a deletion request.
type RequestID = rows.RequestID

// RequestStatus is where a deletion request stands. The zero value is not a status.
type RequestStatus uint8

// The statuses.
const (
	RequestOpen RequestStatus = iota + 1
	RequestWithdrawn
	RequestDeclined
)

var requestStatusWords = [...]string{RequestOpen: rows.StatusOpen, RequestWithdrawn: rows.StatusWithdrawn, RequestDeclined: rows.StatusDeclined}

func (s RequestStatus) valid() bool { return s >= RequestOpen && s <= RequestDeclined }

// String returns "open", "withdrawn", "declined" or "RequestStatus(<n>)". Cannot fail.
func (s RequestStatus) String() string {
	if s.valid() {
		return requestStatusWords[s]
	}
	return fmt.Sprintf("RequestStatus(%d)", uint8(s))
}

// ParseRequestStatus returns the status spelled as String spells it. Fails errs.Invalid.
func ParseRequestStatus(w string) (RequestStatus, error) {
	if s := statusOf(w); s != 0 {
		return s, nil
	}
	return 0, fieldInvalid("datarights.ParseRequestStatus", "status", codeOneOf, "must be one of: open, withdrawn, declined")
}

// Request is a person's deletion request. Kind is "delete" in M2. Note and DecisionNote are
// personal.
type Request struct {
	ID           RequestID
	Person       model.PersonID
	Kind         string
	Status       RequestStatus
	Note         string
	CreatedAt    time.Time
	DecidedAt    time.Time // zero while open
	DecidedBy    model.PersonID
	DecisionNote string
}

// RequestQuery selects requests for ListRequests.
type RequestQuery struct {
	Status RequestStatus  // zero = open
	Person model.PersonID // zero = any
}

// DeletionReport is what Delete, FulfilRequest and the delete command return.
type DeletionReport struct {
	Person   model.PersonID
	People   int                 // rows this package and people deleted: requests, consents, contacts, the person
	Identity int                 // account rows
	Access   int                 // grant rows
	Modules  map[string]Deletion // by source name: every module, "files" and "notify"
	Kept     []Kept              // always includes {"audit.entry", n} when n > 0
}

// ---- helpers ----------------------------------------------------------------------------

// fieldInvalid builds an errs.Invalid error carrying one validate.FieldError.
func fieldInvalid(op, field, code, format string, args ...any) error {
	v := validate.Errors{Op: op}
	v.Addf(field, code, format, args...)
	return v.Err() //nolint:wrapcheck // Err already carries Op and errs.Invalid
}

// fieldConflict builds an errs.Conflict error carrying one validate.FieldError (the api
// shows the fields of a Conflict as it does those of an Invalid).
func fieldConflict(op, field, code string, params map[string]string, reason string) error {
	return errs.E(op, errs.Conflict, &validate.FieldError{Field: field, Code: code, Params: params, Reason: reason})
}

// checkNote validates a request or decision note and returns the trimmed text: valid UTF-8,
// no control characters, at most MaxNoteBytes. Fails errs.Invalid on field.
func checkNote(op, field, s string) (string, error) {
	v := validate.Errors{Op: op}
	s = strings.TrimSpace(s)
	switch {
	case !utf8.ValidString(s):
		v.Addf(field, codeMalformed, "must be valid UTF-8")
	case strings.IndexFunc(s, unicode.IsControl) >= 0:
		v.Addf(field, codeControl, "must not contain control characters")
	case len(s) > MaxNoteBytes:
		v.Add(errs.E("", errs.Invalid, &validate.FieldError{
			Field: field, Code: codeTooLong, Params: map[string]string{"max": fmt.Sprint(MaxNoteBytes)},
			Reason: fmt.Sprintf("must be at most %d bytes", MaxNoteBytes),
		}))
	}
	if err := v.Err(); err != nil {
		return "", err //nolint:wrapcheck // Err already carries Op and errs.Invalid
	}
	return s, nil
}

// statusOf maps a stored status word to the RequestStatus; a word outside the three (the
// table's CHECK forbids it) is the zero status.
func statusOf(word string) RequestStatus {
	for st := RequestOpen; st <= RequestDeclined; st++ {
		if requestStatusWords[st] == word {
			return st
		}
	}
	return 0
}
