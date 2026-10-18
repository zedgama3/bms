package identity

import (
	"time"

	"github.com/zedgama3/bms/internal/core/audit"
	"github.com/zedgama3/bms/internal/core/authz"
	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/identity/postgres"
	"github.com/zedgama3/bms/internal/core/signals"
	"github.com/zedgama3/bms/internal/lib/validate"
	"github.com/zedgama3/bms/internal/model"
)

// Names and limits.
const (
	CookieName              = "__Host-bms_session" // api decision 7
	DevCookieName           = "bms_session_dev"    // api decision 24
	TokenVersion            = 1
	MaxPasswordBytes        = 256 // a longer password is Invalid (too_long); Argon2 has no 72-byte limit
	PasswordMinLengthFloor  = 12  // Policy.PasswordMinLength may not go below this (09 §2)
	GeneratedPasswordLength = 24  // create-superuser and temporary passwords
	HashConcurrency         = 4   // Argon2id hashes in flight per process; more wait on the ctx
	MaxTrackedKeys          = 10000
	MaxPageSize             = 100
	DefaultPageSize         = 50
	ClockSkew               = 5 * time.Minute // a token issued further in the future is malformed
)

// generatedAlphabet has no look-alike characters (no i, l, o, 0, 1) and no upper case, so a
// temporary password can be read out and typed.
const generatedAlphabet = "abcdefghjkmnpqrstuvwxyz23456789"

// Permissions, signal and audit vocabulary identity declares.
const (
	PermissionAccountsRead   authz.Permission = "accounts.read"   // list and view accounts, including the address (personal)
	PermissionAccountsManage authz.Permission = "accounts.manage" // disable, enable, temporary password

	SignalAccountCreated signals.Name = "identity.account-created"
)

// The payload keys of SignalAccountCreated.
const (
	PayloadAccount = "account" // model.AccountID
	PayloadPerson  = "person"  // model.PersonID
)

// Audit actions and the record kinds identity writes.
const (
	actionLogin          audit.Action = "identity.login"
	actionLoginFailed    audit.Action = "identity.login-failed"
	actionLogout         audit.Action = "identity.logout"
	actionCreated        audit.Action = "identity.account.created"
	actionRead           audit.Action = "identity.account.read"
	actionDisabled       audit.Action = "identity.account.disabled"
	actionEnabled        audit.Action = "identity.account.enabled"
	actionDeleted        audit.Action = "identity.account.deleted"
	actionLinked         audit.Action = "identity.account.linked"
	actionPasswordSet    audit.Action = "identity.password.changed"
	actionTemporarySet   audit.Action = "identity.password.temporary-set"
	actionSessionsEnded  audit.Action = "identity.sessions.ended"
	actionSuperCreated   audit.Action = "identity.superuser.created"
	actionSuperReset     audit.Action = "identity.superuser.reset"
	actionSuperuserLogin audit.Action = "identity.superuser.login"

	kindAccount = "identity.account"
	kindSession = "identity.session"
)

// Permissions lists the declarations of PermissionAccountsRead and PermissionAccountsManage.
var Permissions = []authz.PermissionDecl{
	{Name: PermissionAccountsRead, Scope: authz.ScopeSite, Reaches: model.SensitivityPersonal},
	{Name: PermissionAccountsManage, Scope: authz.ScopeSite, Reaches: model.SensitivityPersonal},
}

// Set is the migration set app adds to coreSets (db.md Proposed change 10).
var Set = db.Set{Name: "identity", Schema: "core", Files: postgres.Migrations}

// Field codes identity adds to the shell's vocabulary.
const (
	codeTooShort  = "too_short" // params: min
	codeTooLong   = "too_long"  // params: max
	codeCommon    = "common"    // the password is in the common-password list
	codeTaken     = "taken"     // the address belongs to an account
	codeIncorrect = "incorrect" // the current password is wrong
	codeSelf      = "self"      // an admin change aimed at one's own account
	codeMalformed = validate.CodeMalformed
	codeUnknown   = "unknown"     // no account has the address (LinkAccount)
	codeSuperuser = "superuser"   // the account is the superuser's (LinkAccount)
	codeHasAcct   = "has_account" // the person already has an account (LinkAccount)
	codeRequired  = validate.CodeRequired
)

// emailKey is the unique constraint a duplicate address violates.
const emailKey = "account_email_canonical_key"

// personKey is the unique constraint a second account for one person violates.
const personKey = "account_person_id_key"
