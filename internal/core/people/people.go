package people

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"

	"github.com/zedgama3/bms/internal/contracts/personpage"
	"github.com/zedgama3/bms/internal/core/audit"
	"github.com/zedgama3/bms/internal/core/authz"
	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/identity"
	"github.com/zedgama3/bms/internal/core/people/postgres"
	"github.com/zedgama3/bms/internal/core/people/rows"
	"github.com/zedgama3/bms/internal/core/signals"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/validate"
	"github.com/zedgama3/bms/internal/model"
)

// Limits (bytes, not characters, to match the CHECK constraints) and bounds.
const (
	MaxPlayaNameBytes    = 100
	MaxPronounsBytes     = 40
	MaxLegalNameBytes    = 200
	MaxPhoneBytes        = 40
	MaxRelationshipBytes = 60
	MaxLanguageBytes     = 35
	MaxNoteBytes         = 500
	MaxEmergencyContacts = 10
	MinSearchBytes       = 2
	MaxSearchBytes       = 100
	MaxSearchResults     = 50
	DefaultSearchResults = 20
	MaxNames             = 200
	ExportCooldown       = 10 * time.Minute
	MaxTrackedExports    = 10000
	SectionTimeout       = personpage.SectionTimeout

	// ExportFormat is the "format" of the export document.
	ExportFormat = 1

	// maxRequestsListed bounds MyRequests (a person's requests are finite: one open at a
	// time, the rest decided).
	maxRequestsListed   = 200
	maxPurposeBytes     = 64
	maxContactNameBytes = 200
)

// The permissions people declares; app adds Permissions to corePerms.
const (
	PermissionDirectoryRead  authz.Permission = "people.directory.read"  // search; internal fields of any person
	PermissionContactsRead   authz.Permission = "people.contacts.read"   // personal fields and emergency contacts of others
	PermissionProfilesManage authz.Permission = "people.profiles.manage" // create, change, link people; their consents
	PermissionDataExport     authz.Permission = "people.data.export"     // export everything about any person
	PermissionDataDelete     authz.Permission = "people.data.delete"     // delete a person; decide deletion requests
)

// Permissions lists the declarations of the five permissions above, with their starter
// roles (people.md, "Permissions" and decisions 9 and 20, amended by D44). Note that
// department-admin holds people.contacts.read from D44, and until a module contributes
// people.membership a department extent reaches every person (decision 9).
var Permissions = []authz.PermissionDecl{
	{
		Name: PermissionDirectoryRead, Scope: authz.ScopeDepartment, Reaches: model.SensitivityInternal,
		StarterRoles: []string{authz.RoleAccountAdmin, authz.RoleAccessAdmin, authz.RoleAuditReviewer, authz.RoleDepartmentSteward, authz.RoleDepartmentAdmin, authz.RoleVolunteerCoordinator},
	},
	{
		Name: PermissionContactsRead, Scope: authz.ScopeDepartment, Reaches: model.SensitivityPersonal,
		StarterRoles: []string{authz.RoleAccountAdmin, authz.RoleDepartmentAdmin, authz.RoleVolunteerCoordinator},
	},
	{
		Name: PermissionProfilesManage, Scope: authz.ScopeSite, Reaches: model.SensitivityPersonal,
		StarterRoles: []string{authz.RoleAccountAdmin},
	},
	{
		Name: PermissionDataExport, Scope: authz.ScopeSite, Reaches: model.SensitivitySensitive,
		StarterRoles: []string{authz.RoleAccountAdmin},
	},
	{
		Name: PermissionDataDelete, Scope: authz.ScopeSite, Reaches: model.SensitivitySensitive,
		StarterRoles: []string{authz.RoleAccountAdmin},
	},
}

// The signals people publishes (payload keys below) and the one it subscribes to.
const (
	SignalPersonCreated     signals.Name = "people.person-created"     // {person}
	SignalPersonDeleted     signals.Name = "people.person-deleted"     // {person}
	SignalDeletionRequested signals.Name = "people.deletion-requested" // {request, person}

	PayloadPerson  = "person"
	PayloadRequest = "request"
)

// PublishedSignals lists the signals people publishes, for app's scope and
// Env.CoreSignals; SubscribedSignals the ones it subscribes to. Callers must not modify
// them.
var (
	PublishedSignals  = []signals.Name{SignalPersonCreated, SignalPersonDeleted, SignalDeletionRequested}
	SubscribedSignals = []signals.Name{identity.SignalAccountCreated}
)

// The audit actions people records, and the record kinds they point at.
const (
	actionRead             audit.Action = "people.person.read"
	actionCreated          audit.Action = "people.person.created"
	actionChanged          audit.Action = "people.person.changed"
	actionDeleted          audit.Action = "people.person.deleted"
	actionLinked           audit.Action = "people.person.linked"
	actionExported         audit.Action = "people.person.exported"
	actionSearchRead       audit.Action = "people.search.read"
	actionContactCreated   audit.Action = "people.contact.created"
	actionContactChanged   audit.Action = "people.contact.changed"
	actionContactDeleted   audit.Action = "people.contact.deleted"
	actionConsentChanged   audit.Action = "people.consent.changed"
	actionRequestCreated   audit.Action = "people.request.created"
	actionRequestRead      audit.Action = "people.request.read"
	actionRequestWithdraw  audit.Action = "people.request.withdrawn"
	actionRequestDeclined  audit.Action = "people.request.declined"
	actionRequestFulfilled audit.Action = "people.request.fulfilled"

	kindPerson  = "people.person"
	kindContact = "people.contact"
	kindRequest = "people.request"
)

// Set is the migration set app adds to coreSets (db.md Proposed change 10).
var Set = db.Set{Name: "people", Schema: "core", Files: postgres.Migrations}

// Field codes people adds to the shell's vocabulary.
const (
	codeRequired   = validate.CodeRequired
	codeMalformed  = validate.CodeMalformed
	codeOneOf      = validate.CodeOneOf
	codeRange      = validate.CodeRange
	codeTooLong    = "too_long"      // params: max
	codeTooShort   = "too_short"     // params: min
	codeTooMany    = "too_many"      // params: max
	codeControl    = "control_chars" // a control character in a text field
	codeHidden     = "hidden"        // a field the regional turned off
	codePermanent  = "permanent"     // playa_name once it is set (UpdateMe)
	codeHasAccount = "has_account"   // contact_email for a person who has an account
	codeSelf       = "self"          // an action aimed at oneself that must not be
	codeUnknown    = "unknown"       // an address no account has
	codeDuplicate  = "duplicate"     // an open request exists
	codeStatus     = "status"        // the request is not open
	codeInUse      = "in_use"        // a foreign key still references the person
	codeSuperuser  = "superuser"
	codeProtected  = "protected" // identity's Guard refuses: a shared demo account
	codeLastAdmin  = "last_access_admin"
)

// ---- identifiers ------------------------------------------------------------------------

// ContactID identifies an emergency contact.
type ContactID = rows.ContactID

// RequestID identifies a deletion request.
type RequestID = rows.RequestID

// ---- Purpose ----------------------------------------------------------------------------

// purposeRE is "<owner>.<ident>": the owner is a module name or core area (letters and
// digits), the ident module-contract's kebab-case ident.
var purposeRE = regexp.MustCompile(`^[a-z][a-z0-9]*\.[a-z][a-z0-9]*(-[a-z0-9]+)*$`)

// Purpose names what a person consented to: "<owner>.<ident>", owner a module name or a
// core area, ident per the module-contract grammar; at most 64 bytes.
type Purpose string

// Validate fails errs.Invalid (field "purpose", code "malformed") unless p is well formed.
// The error never echoes p.
func (p Purpose) Validate() error {
	if len(p) <= maxPurposeBytes && purposeRE.MatchString(string(p)) {
		return nil
	}
	return fieldInvalid("people.Purpose.Validate", "purpose", codeMalformed,
		"must be <owner>.<ident>, lower-case letters, digits and single hyphens, at most %d bytes", maxPurposeBytes)
}

// Owner returns the part before the first dot, or "" when p is invalid. Cannot fail.
func (p Purpose) Owner() string {
	if p.Validate() != nil {
		return ""
	}
	owner, _, _ := strings.Cut(string(p), ".")
	return owner
}

// ---- FieldMode and Policy ---------------------------------------------------------------

// FieldMode is a regional's choice for an optional profile field. The zero value is not a
// mode.
type FieldMode uint8

// The modes.
const (
	FieldHidden   FieldMode = iota + 1 // not shown, not stored on save (Invalid "hidden")
	FieldOptional                      // shown, may stay empty
	FieldRequired                      // the person's own save must fill it
)

var fieldModeNames = [...]string{FieldHidden: "hidden", FieldOptional: "optional", FieldRequired: "required"}

func (m FieldMode) valid() bool { return m >= FieldHidden && m <= FieldRequired }

// ParseFieldMode returns the mode spelled "hidden", "optional" or "required". Fails
// errs.Invalid; the error never echoes s.
func ParseFieldMode(s string) (FieldMode, error) {
	for m := FieldHidden; m <= FieldRequired; m++ {
		if fieldModeNames[m] == s {
			return m, nil
		}
	}
	return 0, fieldInvalid("people.ParseFieldMode", "mode", codeOneOf, "must be one of: hidden, optional, required")
}

// String returns "hidden", "optional", "required" or "FieldMode(<n>)". Cannot fail.
func (m FieldMode) String() string {
	if m.valid() {
		return fieldModeNames[m]
	}
	return fmt.Sprintf("FieldMode(%d)", uint8(m))
}

// Validate fails errs.Invalid unless m is one of the three modes.
func (m FieldMode) Validate() error {
	if m.valid() {
		return nil
	}
	return fieldInvalid("people.FieldMode.Validate", "mode", codeOneOf, "must be one of: hidden, optional, required")
}

// Policy is what a regional set (settings people.*). Emergency contacts may be hidden or
// optional, never required (Q-CORE7).
type Policy struct {
	LegalName         FieldMode
	Phone             FieldMode
	EmergencyContacts FieldMode
}

// DefaultPolicy is optional, optional, optional. Cannot fail.
func DefaultPolicy() Policy {
	return Policy{LegalName: FieldOptional, Phone: FieldOptional, EmergencyContacts: FieldOptional}
}

// Validate fails errs.Invalid joining a validate.FieldError per problem: a mode that is
// not one of the three (fields people.legal_name, people.phone, people.emergency_contacts,
// code one_of) and EmergencyContacts == FieldRequired (people.emergency_contacts, one_of).
func (p Policy) Validate() error {
	v := validate.Errors{Op: "people.Policy.Validate"}
	if p.LegalName.Validate() != nil {
		v.Addf("people.legal_name", codeOneOf, "must be one of: hidden, optional, required")
	}
	if p.Phone.Validate() != nil {
		v.Addf("people.phone", codeOneOf, "must be one of: hidden, optional, required")
	}
	switch {
	case p.EmergencyContacts.Validate() != nil:
		v.Addf("people.emergency_contacts", codeOneOf, "must be one of: hidden, optional")
	case p.EmergencyContacts == FieldRequired:
		v.Addf("people.emergency_contacts", codeOneOf, "must be hidden or optional: emergency contacts are never required")
	}
	return v.Err() //nolint:wrapcheck // Err already carries Op and errs.Invalid
}

// ---- records ----------------------------------------------------------------------------

// Person is one profile. Personal fields (LegalName, Phone, ContactEmail, Email) are zero
// when the viewer may not see them; Profile.Fields says which were included.
type Person struct {
	ID           model.PersonID
	PlayaName    string      // internal; "" until filled in
	Pronouns     string      // internal
	Language     string      // internal; BCP 47 tag or "" (the regional's default)
	LegalName    string      // personal
	Phone        string      // personal
	ContactEmail model.Email // personal; only for a person without an account
	Email        model.Email // personal; the login address (identity), zero without an account
	HasAccount   bool
	Complete     bool // PlayaName set and every FieldRequired field set
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// Validate fails errs.Invalid joining a validate.FieldError per problem: a zero ID, a text
// field over its byte limit (too_long), a ContactEmail or Email that is not an address.
// Messages never echo a value.
func (p Person) Validate() error {
	v := validate.Errors{Op: "people.Person.Validate"}
	if p.ID.IsZero() {
		v.Addf("id", codeRequired, "must be set")
	}
	for _, f := range []struct {
		field string
		value string
		limit int
	}{
		{"playa_name", p.PlayaName, MaxPlayaNameBytes},
		{"pronouns", p.Pronouns, MaxPronounsBytes},
		{"language", p.Language, MaxLanguageBytes},
		{"legal_name", p.LegalName, MaxLegalNameBytes},
		{"phone", p.Phone, MaxPhoneBytes},
	} {
		if len(f.value) > f.limit {
			addTooLong(&v, f.field, f.limit)
		}
	}
	if p.ContactEmail != "" && p.ContactEmail.Validate() != nil {
		v.Addf("contact_email", codeMalformed, "must be a valid e-mail address")
	}
	if p.Email != "" && p.Email.Validate() != nil {
		v.Addf("email", codeMalformed, "must be a valid e-mail address")
	}
	return v.Err() //nolint:wrapcheck // Err already carries Op and errs.Invalid
}

// LogValue emits the ID only, so slog never prints a name, number or address. Cannot fail.
func (p Person) LogValue() slog.Value {
	return slog.GroupValue(slog.String("person_id", p.ID.String()))
}

// EmergencyContact is one entry on a person's list. Every text field is personal.
type EmergencyContact struct {
	ID           ContactID
	Person       model.PersonID
	Name         string
	Phone        string
	Relationship string
	Position     int // 1-based display order
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// Consent is the current state of one purpose for one person. The flag is internal.
type Consent struct {
	Person     model.PersonID
	Purpose    Purpose
	Granted    bool
	RecordedBy model.PersonID // the person, or the profile manager who recorded a paper form
	ChangedAt  time.Time
}

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
	return 0, fieldInvalid("people.ParseRequestStatus", "status", codeOneOf, "must be one of: open, withdrawn, declined")
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

// Capabilities tells the shell which actions the viewer may take on this profile. The
// server checks again on every call.
type Capabilities struct {
	Edit, ReadContacts, Export, Delete, Link, ViewRoles, ViewAudit bool
}

// The names Profile.Fields uses.
const (
	FieldPlayaName         = "playa_name"
	FieldPronouns          = "pronouns"
	FieldLanguage          = "language"
	FieldLegalName         = "legal_name"
	FieldPhone             = "phone"
	FieldContactEmail      = "contact_email"
	FieldEmail             = "email"
	FieldEmergencyContacts = "emergency_contacts"
	FieldConsents          = "consents"
)

// Profile is what Me and Get return: the person as the viewer may see them, the field
// names included, their contacts (only when ReadContacts or self), consents (self or
// profiles.manage), the modules' sections, what the viewer may do, and the field policy so
// the form knows what to show and require.
type Profile struct {
	Person   Person
	Fields   []string
	Contacts []EmergencyContact
	Consents []Consent
	Sections []personpage.Section
	Can      Capabilities
	Policy   Policy
}

// ProfileInput is a partial update: nil leaves a field unchanged; a pointer to "" clears
// it.
type ProfileInput struct {
	PlayaName    *string
	Pronouns     *string
	Language     *string
	LegalName    *string
	Phone        *string
	ContactEmail *string
}

// ContactInput is the body of AddContact and UpdateContact.
type ContactInput struct {
	Name         string
	Phone        string
	Relationship string
}

// SearchQuery is one Search.
type SearchQuery struct {
	Q     string // MinSearchBytes..MaxSearchBytes after trimming
	Limit int    // 0 = DefaultSearchResults; at most MaxSearchResults
}

// Match is a search result: internal fields only.
type Match struct {
	ID        model.PersonID
	PlayaName string
	Pronouns  string
}

// RequestQuery selects requests for ListRequests.
type RequestQuery struct {
	Status RequestStatus  // zero = open
	Person model.PersonID // zero = any
}

// LinkResult is what LinkAccount reports.
type LinkResult struct {
	Account       model.AccountID
	Orphan        model.PersonID // the row sign-up minted for the account; zero if none
	OrphanDeleted bool
}

// Export is a module's exported data, ModuleDeletion what its DeletePerson did and Kept a
// kind of record it kept: structural twins of module.Export, module.Deletion and module.Kept
// (this package never imports core/module; app converts).
type Export struct{ Data json.RawMessage }

// Kept is one kind of record that deletion left in place, with how many.
type Kept struct {
	Kind  string
	Count int
}

// ModuleDeletion is what one module's DeletePerson did.
type ModuleDeletion struct {
	Deleted, Anonymized int
	Kept                []Kept
}

// DeletionReport is what Delete, FulfilRequest and the delete command return.
type DeletionReport struct {
	Person   model.PersonID
	People   int                       // rows this package deleted
	Identity int                       // account rows
	Access   int                       // grant rows
	Modules  map[string]ModuleDeletion // by module name
	Kept     []Kept                    // always includes {"audit.entry", n} when n > 0
}

// auditEntryKind is the Kept kind of the audit rows deletion leaves (audit decision 10).
const auditEntryKind = "audit.entry"

// ---- text validation --------------------------------------------------------------------

var (
	phoneRE    = regexp.MustCompile(`^[+0-9][0-9 ().\-]*(x[0-9]{1,6})?$`)
	languageRE = regexp.MustCompile(`^[a-z]{2,3}(-[A-Za-z0-9]{1,8})*$`)
)

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

// NameKey (decision 31) is the form in which two playa names are compared: NFKC
// normalisation, leading and trailing space removed, each run of internal whitespace
// replaced by one space, then lower-cased with strings.ToLower. "" for a blank name. It is
// the one rule for "the same name": the store writes it to core.person.playa_name_key, the
// unique index person_playa_name_key_idx enforces it, and modules that compare playa names
// use it rather than their own copy. Pure: no I/O, cannot fail; the result may be longer
// than MaxPlayaNameBytes (NFKC can expand a character), which no constraint limits because
// the key is never shown or typed. Never logged: a key is a name.
func NameKey(name string) string {
	return strings.ToLower(strings.Join(strings.Fields(norm.NFKC.String(name)), " "))
}

// fieldConflict builds an errs.Conflict error carrying one validate.FieldError (the api
// shows the fields of a Conflict as it does those of an Invalid).
func fieldConflict(op, field, code string, params map[string]string, reason string) error {
	return errs.E(op, errs.Conflict, &validate.FieldError{Field: field, Code: code, Params: params, Reason: reason})
}

// checkText trims s and checks it: valid UTF-8, no control characters, at most max bytes.
// Problems are added to v under field; the trimmed text is returned either way.
func checkText(v *validate.Errors, field, s string, limit int) string {
	s = strings.TrimSpace(s)
	switch {
	case !utf8.ValidString(s):
		v.Addf(field, codeMalformed, "must be valid UTF-8")
	case strings.IndexFunc(s, unicode.IsControl) >= 0:
		v.Addf(field, codeControl, "must not contain control characters")
	case len(s) > limit:
		addTooLong(v, field, limit)
	}
	return s
}

func checkPhone(v *validate.Errors, field, s string) string {
	s = checkText(v, field, s, MaxPhoneBytes)
	if s != "" && len(s) <= MaxPhoneBytes && !phoneRE.MatchString(s) {
		v.Addf(field, codeMalformed, "must look like a phone number")
	}
	return s
}

func checkLanguage(v *validate.Errors, field, s string) string {
	s = checkText(v, field, s, MaxLanguageBytes)
	if s != "" && len(s) <= MaxLanguageBytes && !languageRE.MatchString(s) {
		v.Addf(field, codeMalformed, "must be a language tag such as en or pt-BR")
	}
	return s
}

// profileFields are the editable fields of a ProfileInput, by name.
type profileFields struct {
	playaName, pronouns, language, legalName, phone, contactEmail *string
}

// languageOnly says the body sets the language and nothing else, the one save the shell
// makes before the profile is complete (D37).
func (f profileFields) languageOnly() bool {
	return f.language != nil && f.playaName == nil && f.pronouns == nil && f.legalName == nil &&
		f.phone == nil && f.contactEmail == nil
}

// normalize validates in field by field and returns the trimmed values. Every problem is
// reported at once (F6). Nothing is checked against the policy here.
func (in ProfileInput) normalize(op string) (profileFields, error) {
	v := validate.Errors{Op: op}
	var out profileFields
	text := func(field string, p *string, limit int) *string {
		if p == nil {
			return nil
		}
		s := checkText(&v, field, *p, limit)
		return &s
	}
	out.playaName = text(FieldPlayaName, in.PlayaName, MaxPlayaNameBytes)
	out.pronouns = text(FieldPronouns, in.Pronouns, MaxPronounsBytes)
	out.legalName = text(FieldLegalName, in.LegalName, MaxLegalNameBytes)
	if in.Language != nil {
		s := checkLanguage(&v, FieldLanguage, *in.Language)
		out.language = &s
	}
	if in.Phone != nil {
		s := checkPhone(&v, FieldPhone, *in.Phone)
		out.phone = &s
	}
	if in.ContactEmail != nil {
		s := strings.TrimSpace(*in.ContactEmail)
		if s != "" {
			if _, err := model.ParseEmail(s); err != nil {
				v.Addf(FieldContactEmail, codeMalformed, "must be a valid e-mail address")
			}
		}
		out.contactEmail = &s
	}
	if err := v.Err(); err != nil {
		return profileFields{}, err //nolint:wrapcheck // Err already carries Op and errs.Invalid
	}
	return out, nil
}

// validate checks a contact input and returns the trimmed copy.
func (in ContactInput) normalize(op string) (ContactInput, error) {
	v := validate.Errors{Op: op}
	out := ContactInput{
		Name:         checkText(&v, "name", in.Name, maxContactNameBytes),
		Phone:        checkPhone(&v, "phone", in.Phone),
		Relationship: checkText(&v, "relationship", in.Relationship, MaxRelationshipBytes),
	}
	if out.Name == "" {
		v.Addf("name", codeRequired, "must not be empty")
	}
	if out.Phone == "" {
		v.Addf("phone", codeRequired, "must not be empty")
	}
	if err := v.Err(); err != nil {
		return ContactInput{}, err //nolint:wrapcheck // Err already carries Op and errs.Invalid
	}
	return out, nil
}

// checkNote validates a request or decision note and returns the trimmed text.
func checkNote(op, field, s string) (string, error) {
	v := validate.Errors{Op: op}
	s = checkText(&v, field, s, MaxNoteBytes)
	if err := v.Err(); err != nil {
		return "", err //nolint:wrapcheck // Err already carries Op and errs.Invalid
	}
	return s, nil
}
