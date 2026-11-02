package files

import (
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/zedgama3/bms/internal/core/audit"
	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/files/postgres"
	"github.com/zedgama3/bms/internal/core/objects"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/validate"
	"github.com/zedgama3/bms/internal/model"
)

// Limits, deadlines and names (files.md, "Exported").
const (
	// MaxNameBytes is the longest stored file name.
	MaxNameBytes = 255
	// MaxFields is the most text fields ReadUpload collects before the file part.
	MaxFields = 32
	// MaxFieldBytes is the longest text field value.
	MaxFieldBytes = 4096
	// SniffBytes is how much of a file Detect looks at.
	SniffBytes = 512
	// UploadCeiling is the request body cap of an upload route (Route.MaxBodyBytes).
	UploadCeiling = 1 << 30
	// UploadTimeout is the deadline of an upload route (Route.Timeout).
	UploadTimeout = 10 * time.Minute
	// ServeTimeout extends the connection's write deadline while a file is sent.
	ServeTimeout = 10 * time.Minute
	// OrphanAge is how long a Staged object is kept before the sweep may remove it.
	OrphanAge = 24 * time.Hour
	// MaxFilesPerRecordCeiling is the largest files.max_per_record.
	MaxFilesPerRecordCeiling = 200

	// JobReap and JobSweepOrphans are the two jobs Start registers through Options.Jobs.
	JobReap         = "files.reap"
	ReapEvery       = time.Hour
	JobSweepOrphans = "files.sweep-orphans"
	SweepEvery      = 24 * time.Hour
	// JobTimeout is the deadline of both jobs.
	JobTimeout = 30 * time.Minute

	// LogoRecordKind is the record kind the regional logo is stored under (settings).
	LogoRecordKind = "settings.logo"
)

// Set is the migration set of files (core.file, core.file_tombstone): after jobs and notify
// in the core set order. No table holds bytes.
var Set = db.Set{Name: "files", Schema: "core", Files: postgres.Migrations}

// The audit actions files records, all in area "files".
const (
	actionCreated  audit.Action = "files.file.created"
	actionDeleted  audit.Action = "files.file.deleted"
	actionRead     audit.Action = "files.file.read"
	actionSwept    audit.Action = "files.sweep.ran"
	actionVerified audit.Action = "files.verify.ran"

	kindFile = "files.file"
)

type fileTag struct{}

// FileID identifies one stored file. It is also the object's key.
type FileID = model.ID[fileTag]

// File is one attachment's metadata. Name is personal data when Level is personal or
// sensitive (a certificate is often named after its holder); LogValue renders the ID only.
type File struct {
	ID          FileID
	Record      audit.Ref      // the owning record; Kind's area is the owner (decision 1)
	Subject     model.PersonID // whom the file is about; zero for nobody in particular
	UploadedBy  model.PersonID // zero after the uploader is deleted, or for an operator
	Level       model.Sensitivity
	Name        string // cleaned base name with the kind's extension
	ContentType string // the sniffed type
	Size        int64
	SHA256      [32]byte
	Stripped    bool // metadata was removed at upload
	CreatedAt   time.Time
}

// Validate fails errs.Invalid joining one FieldError per problem: id, record, level, name,
// content_type, size, sha256 and created_at.
func (f File) Validate() error {
	v := validate.Errors{Op: "files.File.Validate"}
	if f.ID.IsZero() {
		v.Addf("id", validate.CodeRequired, "must be set")
	}
	if f.Record.IsZero() || f.Record.Validate() != nil {
		v.Addf("record", validate.CodeMalformed, "must be a valid record reference")
	}
	if f.Level.Validate() != nil {
		v.Addf("level", validate.CodeOneOf, "must be a sensitivity level")
	}
	if err := checkStoredName(f.Name); err != nil {
		v.Add(err)
	}
	if f.ContentType == "" {
		v.Addf("content_type", validate.CodeRequired, "must be set")
	}
	if f.Size <= 0 {
		v.Addf("size", validate.CodePositive, "must be positive")
	}
	if f.SHA256 == ([32]byte{}) {
		v.Addf("sha256", validate.CodeRequired, "must be set")
	}
	if f.CreatedAt.IsZero() {
		v.Addf("created_at", validate.CodeRequired, "must be set")
	}
	return v.Err() //nolint:wrapcheck // Err already carries Op and errs.Invalid
}

// LogValue renders the ID only: a name is personal data and never reaches a log.
func (f File) LogValue() slog.Value { return slog.StringValue(f.ID.String()) }

func checkStoredName(name string) error {
	switch {
	case name == "":
		return fieldErr("files.File.Validate", errs.Invalid, "name", validate.CodeRequired, nil, "must be set")
	case len(name) > MaxNameBytes:
		return fieldErr("files.File.Validate", errs.Invalid, "name", codeTooLong, maxParam(MaxNameBytes), "must be at most %d bytes", MaxNameBytes)
	case strings.ContainsFunc(name, isControlOrSep):
		return fieldErr("files.File.Validate", errs.Invalid, "name", codeControlChars, nil, "must not contain control characters or path separators")
	}
	return nil
}

func isControlOrSep(r rune) bool { return r < 0x20 || r == 0x7f || r == '/' || r == '\\' }

// Upload is the bytes a caller gives Stage. Body is read once, to the end; the caller owns it.
type Upload struct {
	Name         string // the client's file name; cleaned; "" becomes "file"
	Accept       Accept
	KeepMetadata bool // keep EXIF/XMP (decision 9); false for anything about people
	Body         io.Reader
}

// Validate checks everything but Body and the bytes: Accept (field "accept") and the name
// (field "name", codes "too_long" with param max, and "control_chars").
func (u Upload) Validate() error {
	v := validate.Errors{Op: "files.Upload.Validate"}
	for _, fe := range validate.Fields(u.Accept.Validate()) {
		v.Add(errs.E("", errs.Invalid, fe))
	}
	if _, err := cleanName(u.Name); err != nil {
		for _, fe := range validate.Fields(err) {
			v.Add(errs.E("", errs.Invalid, fe))
		}
	}
	return v.Err() //nolint:wrapcheck // Err already carries Op and errs.Invalid
}

// cleanName reduces a client's file name to its base name: path separators of either kind
// dropped with everything before the last, spaces trimmed, invalid UTF-8 replaced. It fails
// errs.Invalid "name" for control characters and for a base name over MaxNameBytes. An empty
// result is "file".
func cleanName(raw string) (string, error) {
	const op = "files.cleanName"
	if strings.ContainsFunc(raw, func(r rune) bool { return r < 0x20 || r == 0x7f }) {
		return "", fieldErr(op, errs.Invalid, "name", codeControlChars, nil, "must not contain control characters")
	}
	name := strings.ReplaceAll(raw, "\\", "/")
	if i := strings.LastIndex(name, "/"); i >= 0 {
		name = name[i+1:]
	}
	if !utf8.ValidString(name) {
		name = strings.ToValidUTF8(name, "_")
	}
	name = strings.TrimSpace(name)
	if name == "." || name == ".." {
		name = ""
	}
	if len(name) > MaxNameBytes {
		return "", fieldErr(op, errs.Invalid, "name", codeTooLong, maxParam(MaxNameBytes), "must be at most %d bytes", MaxNameBytes)
	}
	if name == "" {
		name = "file"
	}
	return name, nil
}

// storedName forces kind's extension on a cleaned name, replacing the last extension, and
// keeps the result within MaxNameBytes by shortening the stem.
func storedName(clean string, k Kind) string {
	stem := clean
	if i := strings.LastIndex(clean, "."); i >= 0 {
		stem = clean[:i]
	}
	if stem == "" {
		stem = "file"
	}
	ext := k.Extension()
	for len(stem)+len(ext) > MaxNameBytes {
		_, size := utf8.DecodeLastRuneInString(stem)
		stem = stem[:len(stem)-size]
	}
	return stem + ext
}

// Staged is an object that Stage put and nobody has attached: a value, not a row. It is valid
// for the Store that made it, for OrphanAge; after that the sweep may remove the object. The
// handler defers Discard; Discard after Attach is a no-op.
type Staged struct {
	Bucket      objects.Bucket
	Key         objects.Key
	Name        string
	ContentType string
	Size        int64
	SHA256      [32]byte
	Stripped    bool
	StagedAt    time.Time
}

// Validate fails errs.Invalid (field "staged", code "required" for the zero value,
// "malformed" for a bucket, key (which must be a file ID), name, type, size or checksum that
// no Stage would produce).
func (s Staged) Validate() error {
	const op = "files.Staged.Validate"
	if s == (Staged{}) {
		return fieldErr(op, errs.Invalid, "staged", validate.CodeRequired, nil, "must be set")
	}
	bad := s.Bucket.Validate() != nil || s.Key.Validate() != nil ||
		s.Size <= 0 || s.SHA256 == ([32]byte{}) || s.StagedAt.IsZero() ||
		checkStoredName(s.Name) != nil
	if !bad {
		_, err := model.ParseID[FileID](string(s.Key))
		bad = err != nil
	}
	if !bad {
		_, ok := kindOf(s.ContentType)
		bad = !ok
	}
	if bad {
		return fieldErr(op, errs.Invalid, "staged", validate.CodeMalformed, nil, "is not an object Stage produced")
	}
	return nil
}

// Attachment is the record a Staged object is bound to.
type Attachment struct {
	Record  audit.Ref
	Subject model.PersonID
	Level   model.Sensitivity
	Replace bool // delete the record's existing files in the same transaction
}

// Validate fails errs.Invalid on the fields "record" (code "required" or "malformed") and
// "level" (code "one_of"). The owner scope is checked by Attach, which knows the owner.
func (a Attachment) Validate() error {
	const op = "files.Attachment.Validate"
	v := validate.Errors{Op: op}
	switch {
	case a.Record.IsZero():
		v.Addf("record", validate.CodeRequired, "must be set")
	case a.Record.Validate() != nil:
		v.Addf("record", validate.CodeMalformed, "must be <area>.<ident> with an id")
	}
	if a.Level.Validate() != nil {
		v.Addf("level", validate.CodeOneOf, "must be one of: public, internal, personal, sensitive")
	}
	return v.Err() //nolint:wrapcheck // Err already carries Op and errs.Invalid
}

// Policy is the regional's limits (the three settings).
type Policy struct {
	MaxBytes     int64 // per file
	QuotaBytes   int64 // total stored, site-wide; 0 = unlimited
	MaxPerRecord int
}

// DefaultPolicy is 25 MiB per file, no quota, 20 files per record.
func DefaultPolicy() Policy {
	return Policy{MaxBytes: 25 << 20, QuotaBytes: 0, MaxPerRecord: 20}
}

// Validate fails errs.Invalid on max_bytes (1..UploadCeiling), quota_bytes (0 or more) and
// max_per_record (1..MaxFilesPerRecordCeiling).
func (p Policy) Validate() error {
	v := validate.Errors{Op: "files.Policy.Validate"}
	v.Add(validate.Range("max_bytes", p.MaxBytes, 1, int64(UploadCeiling)))
	v.Add(validate.Range("quota_bytes", p.QuotaBytes, 0, int64(1<<63-1)))
	v.Add(validate.Range("max_per_record", p.MaxPerRecord, 1, MaxFilesPerRecordCeiling))
	return v.Err() //nolint:wrapcheck // Err already carries Op and errs.Invalid
}

// Deletion is what DeletePerson did: files deletes nothing of its own there (Deleted is
// always 0) and clears the uploader of the files the person put in.
type Deletion struct{ Deleted, Anonymized int }

// Swept counts what Reap or SweepOrphans removed.
type Swept struct{ Tombstones, Orphans, TempFiles int }

// Verified counts what Verify found.
type Verified struct{ Checked, Missing, Mismatched, Tombstones int }

// Codes set by this package beyond validate's.
const (
	codeTooLong      = "too_long"
	codeTooLarge     = "too_large"
	codeControlChars = "control_chars"
	codeType         = "type"
	codeMalformed    = validate.CodeMalformed
	codeRequired     = validate.CodeRequired
	codeScope        = "scope"
	codeForeign      = "foreign"
	codeExpired      = "expired"
	codeUnknown      = "unknown"
	codeOrder        = "order"
	codeTooMany      = "too_many"
	codeQuota        = "quota"
	codeInUse        = "in_use"
)

func maxParam(n int64) map[string]string { return map[string]string{"max": strconv.FormatInt(n, 10)} }

// fieldErr is one FieldError wrapped in kind: Invalid for a bad input, Conflict for a
// limit that was reached. The reason never echoes a value.
func fieldErr(op string, kind errs.Kind, field, code string, params map[string]string, format string, args ...any) error {
	return errs.E(op, kind, &validate.FieldError{Field: field, Code: code, Params: params, Reason: fmt.Sprintf(format, args...)})
}

func hexOf(sum [32]byte) string { return hex.EncodeToString(sum[:]) }
