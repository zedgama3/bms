package departments

import (
	"fmt"
	"log/slog"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/zedgama3/bms/internal/core/audit"
	"github.com/zedgama3/bms/internal/core/authz"
	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/departments/postgres"
	"github.com/zedgama3/bms/internal/core/departments/rows"
	"github.com/zedgama3/bms/internal/core/signals"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/validate"
	"github.com/zedgama3/bms/internal/model"
)

// Limits (bytes, not characters, to match the CHECK constraints) and bounds.
const (
	MaxNameBytes        = 100
	MaxDescriptionBytes = 2000
	MaxNames            = 200

	// maxContactEmailBytes is model.Email's own limit and the column's.
	maxContactEmailBytes = 254
)

// The permissions departments declares; app adds Permissions to corePerms.
const (
	PermissionManage        authz.Permission = "departments.manage"         // create, rename, archive, restore; see archived ones
	PermissionDetailsManage authz.Permission = "departments.details.manage" // change one department's description and mailbox
)

// Permissions lists the declarations of the two permissions above, with their starter
// roles (departments-events.md, "Permissions").
var Permissions = []authz.PermissionDecl{
	{
		Name: PermissionManage, Scope: authz.ScopeSite, Reaches: model.SensitivityInternal,
		StarterRoles: []string{authz.RoleDepartmentSteward},
	},
	{
		Name: PermissionDetailsManage, Scope: authz.ScopeDepartment, Reaches: model.SensitivityInternal,
		StarterRoles: []string{authz.RoleDepartmentAdmin, authz.RoleDepartmentSteward},
	},
}

// The signals departments publishes; the payload carries the department ID only.
const (
	SignalCreated  signals.Name = "departments.department-created"
	SignalArchived signals.Name = "departments.department-archived"
	SignalRestored signals.Name = "departments.department-restored"

	PayloadDepartment = "department"
)

// PublishedSignals lists the signals departments publishes, for app's scope and
// Env.CoreSignals. Callers must not modify it.
var PublishedSignals = []signals.Name{SignalCreated, SignalArchived, SignalRestored}

// The audit actions departments records, and the record kind they point at.
const (
	actionCreated  audit.Action = "departments.department.created"
	actionChanged  audit.Action = "departments.department.changed"
	actionRenamed  audit.Action = "departments.department.renamed"
	actionArchived audit.Action = "departments.department.archived"
	actionRestored audit.Action = "departments.department.restored"

	kindDepartment = "departments.department"
)

// Set is the migration set app adds to coreSets.
var Set = db.Set{Name: "departments", Schema: "core", Files: postgres.Migrations}

// Field names, as the audit entries and the API name them.
const (
	fieldName         = "name"
	fieldDescription  = "description"
	fieldContactEmail = "contact_email"
)

// Field codes departments adds to the shell's vocabulary.
const (
	codeRequired  = validate.CodeRequired
	codeMalformed = validate.CodeMalformed
	codeTooLong   = "too_long"      // params: max
	codeTooMany   = "too_many"      // params: max
	codeControl   = "control_chars" // a control character in a text field
	codeTaken     = "taken"         // the name belongs to another department
	codeArchived  = "archived"      // the department is archived
	codeNotArch   = "not_archived"  // the department is active
)

// Department is a standing part of the regional. Nothing in it is personal data:
// ContactEmail is the department's shared mailbox, never a person's address.
type Department struct {
	ID           model.DepartmentID `json:"id"`
	Name         string             `json:"name"`
	Description  string             `json:"description"`
	ContactEmail model.Email        `json:"contact_email,omitzero"`
	Archived     bool               `json:"archived"`
	ArchivedAt   time.Time          `json:"archived_at,omitzero"`
	CreatedAt    time.Time          `json:"created_at"`
	UpdatedAt    time.Time          `json:"updated_at"`
}

// Validate fails errs.Invalid joining a validate.FieldError per problem, on the fields
// name (required, too_long, control_chars, malformed), description (too_long,
// control_chars, malformed) and contact_email (malformed, too_long). Messages never echo a
// value.
func (d Department) Validate() error {
	v := validate.Errors{Op: "departments.Department.Validate"}
	checkName(&v, fieldName, d.Name)
	checkDescription(&v, fieldDescription, d.Description)
	checkEmail(&v, fieldContactEmail, string(d.ContactEmail))
	return v.Err() //nolint:wrapcheck // Err already carries Op and errs.Invalid
}

// LogValue emits the ID only, so slog never prints a department's name. Cannot fail.
func (d Department) LogValue() slog.Value {
	return slog.GroupValue(slog.String("department_id", d.ID.String()))
}

// CreateInput creates a department. ContactEmail may be empty.
type CreateInput struct {
	Name         string `json:"name"`
	Description  string `json:"description"`
	ContactEmail string `json:"contact_email"`
}

// UpdateInput changes a department; nil means unchanged. Setting Name needs
// departments.manage; the others departments.details.manage at the department.
type UpdateInput struct {
	Name         *string `json:"name"`
	Description  *string `json:"description"`
	ContactEmail *string `json:"contact_email"`
}

// ListQuery filters List. IncludeArchived needs departments.manage.
type ListQuery struct {
	IncludeArchived bool
}

// departmentOf converts a row.
func departmentOf(r rows.Department) Department {
	return Department{
		ID: r.ID, Name: r.Name, Description: r.Description, ContactEmail: model.Email(r.ContactEmail),
		Archived: !r.ArchivedAt.IsZero(), ArchivedAt: r.ArchivedAt, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
	}
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

// checkText checks s as it is (no trimming): valid UTF-8, no control characters (newline,
// carriage return and tab pass when multiline), at most limit bytes.
func checkText(v *validate.Errors, field, s string, limit int, multiline bool) {
	switch {
	case !utf8.ValidString(s):
		v.Addf(field, codeMalformed, "must be valid UTF-8")
		return
	case strings.IndexFunc(s, func(r rune) bool {
		if multiline && (r == '\n' || r == '\r' || r == '\t') {
			return false
		}
		return unicode.IsControl(r)
	}) >= 0:
		v.Addf(field, codeControl, "must not contain control characters")
		return
	}
	if len(s) > limit {
		addTooLong(v, field, limit)
	}
}

func checkName(v *validate.Errors, field, s string) {
	if s == "" {
		v.Addf(field, codeRequired, "must not be empty")
		return
	}
	checkText(v, field, s, MaxNameBytes, false)
}

func checkDescription(v *validate.Errors, field, s string) {
	checkText(v, field, s, MaxDescriptionBytes, true)
}

// checkEmail accepts "" (no mailbox) or an address of the shape model.ParseEmail accepts.
func checkEmail(v *validate.Errors, field, s string) {
	switch {
	case s == "":
	case len(s) > maxContactEmailBytes:
		addTooLong(v, field, maxContactEmailBytes)
	case model.Email(s).Validate() != nil:
		v.Addf(field, codeMalformed, "must be a valid e-mail address")
	}
}

// normalized is the trimmed, validated form of an input: nil fields stay nil.
type normalized struct {
	name, description, contactEmail *string
}

// normalize trims the inputs and checks every one, reporting all problems at once.
func (in CreateInput) normalize(op string) (normalized, error) {
	return UpdateInput{Name: &in.Name, Description: &in.Description, ContactEmail: &in.ContactEmail}.normalize(op, false)
}

// normalize trims the set fields and checks each. With needOne, an input with no field set
// fails Invalid (body, required). A name is required when set.
func (in UpdateInput) normalize(op string, needOne bool) (normalized, error) {
	v := validate.Errors{Op: op}
	var out normalized
	trim := func(p *string) *string {
		if p == nil {
			return nil
		}
		s := strings.TrimSpace(*p)
		return &s
	}
	out.name, out.description, out.contactEmail = trim(in.Name), trim(in.Description), trim(in.ContactEmail)
	if needOne && out.name == nil && out.description == nil && out.contactEmail == nil {
		v.Addf("body", codeRequired, "set at least one field")
	}
	if out.name != nil {
		checkName(&v, fieldName, *out.name)
	}
	if out.description != nil {
		checkDescription(&v, fieldDescription, *out.description)
	}
	if out.contactEmail != nil {
		checkEmail(&v, fieldContactEmail, *out.contactEmail)
	}
	if err := v.Err(); err != nil {
		return normalized{}, err //nolint:wrapcheck // Err already carries Op and errs.Invalid
	}
	return out, nil
}
