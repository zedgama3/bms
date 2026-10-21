// Package objects is the object store: a simplified S3 (a bucket, a key, Put, Get, Head,
// Delete, List, a SHA-256 verified at Put) that keeps bytes outside the database (D43). It
// is the contract; internal/core/objects/disk (the default) and internal/core/objects/s3 (any
// S3-compatible store) implement it, and objectstest.Conformance is the suite every
// implementation passes. The store knows no person and decides nothing: callers (the files
// service, and app for any later core need for bytes) own permissions, levels and audit.
package objects

import (
	"context"
	"errors"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/validate"
)

// Sizes and names the contract pins.
const (
	// MaxKeyBytes is the longest Key.
	MaxKeyBytes = 255
	// MaxBucketBytes is the longest Bucket.
	MaxBucketBytes = 32
	// ProbeKey is the object Ping writes to prove the store is writable and then reads
	// anonymously to prove it is not public (s3 only; disk never checks it).
	ProbeKey Key = ".bms-probe"
)

// Codes set by this package, for the web shell and callers to branch on.
const (
	codeMalformed = validate.CodeMalformed
	codeRequired  = validate.CodeRequired
	codeMismatch  = "mismatch"
	codeTooLong   = "too_long"
)

// Bucket is an owner's namespace: [a-z][a-z0-9]{0,31}. Implementations map it to a
// directory (disk) or a key prefix inside one real bucket (s3).
type Bucket string

// Validate fails errs.Invalid (field "bucket", code "required" or "malformed"). It never
// echoes the value. Cannot fail otherwise.
func (b Bucket) Validate() error {
	const op = "objects.Bucket.Validate"
	switch {
	case b == "":
		return fieldErr(op, "bucket", codeRequired, "must be set")
	case len(b) > MaxBucketBytes || !validBucket(string(b)):
		return fieldErr(op, "bucket", codeMalformed,
			"must be a lower-case letter then up to %d lower-case letters or digits", MaxBucketBytes-1)
	}
	return nil
}

func validBucket(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z':
		case c >= '0' && c <= '9' && i > 0:
		default:
			return false
		}
	}
	return len(s) > 0
}

// Key names one object inside a bucket: 1..MaxKeyBytes bytes of [A-Za-z0-9._/-], with no
// empty, "." or ".." segment. files uses the file ID.
type Key string

// Validate fails errs.Invalid (field "key", code "required", "too_long" or "malformed").
// It never echoes the value.
func (k Key) Validate() error {
	const op = "objects.Key.Validate"
	switch {
	case k == "":
		return fieldErr(op, "key", codeRequired, "must be set")
	case len(k) > MaxKeyBytes:
		return fieldErr(op, "key", codeTooLong, "must be at most %d bytes", MaxKeyBytes)
	}
	for i := 0; i < len(k); i++ {
		c := k[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		case c == '.', c == '_', c == '/', c == '-':
		default:
			return fieldErr(op, "key", codeMalformed, "may contain only A-Z a-z 0-9 . _ / and -")
		}
	}
	for seg := range strings.SplitSeq(string(k), "/") {
		if seg == "" || seg == "." || seg == ".." {
			return fieldErr(op, "key", codeMalformed, "must have no empty, . or .. segment")
		}
	}
	return nil
}

// Kind names an implementation.
type Kind uint8

// The implementations.
const (
	Disk Kind = iota + 1
	S3
)

// ParseKind returns the Kind named "disk" or "s3". Fails errs.Invalid (field "store",
// code "one_of").
func ParseKind(s string) (Kind, error) {
	switch s {
	case "disk":
		return Disk, nil
	case "s3":
		return S3, nil
	}
	return 0, fieldErr("objects.ParseKind", "store", validate.CodeOneOf, "must be one of: disk, s3")
}

// String returns "disk", "s3" or "Kind(<n>)". Cannot fail.
func (k Kind) String() string {
	switch k {
	case Disk:
		return "disk"
	case S3:
		return "s3"
	}
	return "Kind(" + strconv.Itoa(int(k)) + ")"
}

// Info is what the store knows about one object. SHA256 is the checksum verified at Put;
// a store that cannot return it answers the zero value.
type Info struct {
	Bucket      Bucket
	Key         Key
	Size        int64
	SHA256      [32]byte
	ContentType string
	Modified    time.Time
}

// Entry is one List row: what a listing can give cheaply in every store.
type Entry struct {
	Key      Key
	Size     int64
	Modified time.Time
}

// MaxContentTypeBytes is the longest PutInput.ContentType.
const MaxContentTypeBytes = 127

// PutInput is a complete object: the caller knows the size and the checksum up front (files
// spools first). Body is read exactly Size bytes; the caller owns it.
type PutInput struct {
	Bucket      Bucket
	Key         Key
	Size        int64
	SHA256      [32]byte
	ContentType string
	Body        io.Reader
}

// Validate checks everything but the bytes. It fails errs.Invalid joining one FieldError
// per problem: "bucket", "key", "size" (negative) and "content_type" (control characters or
// too long). The checksum is checked against the bytes by Put (a zero one never matches).
func (p PutInput) Validate() error {
	v := validate.Errors{Op: "objects.PutInput.Validate"}
	addFields(&v, p.Bucket.Validate())
	addFields(&v, p.Key.Validate())
	if p.Size < 0 {
		v.Addf("size", validate.CodeRange, "must not be negative")
	}
	if len(p.ContentType) > MaxContentTypeBytes || strings.ContainsFunc(p.ContentType, func(r rune) bool { return r < 0x20 || r == 0x7f }) {
		v.Addf("content_type", codeMalformed, "must be at most %d bytes with no control characters", MaxContentTypeBytes)
	}
	return v.Err() //nolint:wrapcheck // Err already carries Op and errs.Invalid
}

func addFields(v *validate.Errors, err error) {
	for _, fe := range validate.Fields(err) {
		v.Add(errs.E("", errs.Invalid, fe))
	}
}

// Object is Info plus the bytes, seekable for ranges. The caller closes Body.
type Object struct {
	Info
	Body io.ReadSeekCloser
}

// Store is the simplified S3 the core and app use. Every implementation passes
// objectstest.Conformance and is safe for concurrent use. Every method honours ctx and
// fails errs.Timeout or errs.Canceled when it ends first.
type Store interface {
	// Put stores exactly in.Size bytes under (Bucket, Key), verifying in.SHA256 as it goes:
	// a key that exists is Conflict; a short or long Body or a checksum mismatch is Invalid
	// (fields "size", "checksum") and nothing is kept. Consumer: files.Stage.
	Put(ctx context.Context, in PutInput) (Info, error)
	// Get answers the object; a missing one is Unavailable wrapping ErrMissing.
	// Consumers: files.Open, files.ExportPerson.
	Get(ctx context.Context, b Bucket, k Key) (Object, error)
	// Head answers Info without the bytes; missing as Get. Consumer: files.Verify.
	Head(ctx context.Context, b Bucket, k Key) (Info, error)
	// Delete removes the object; a missing one is not an error. Consumers: files.Discard,
	// the AfterCommit of files.Delete, files.Reap, files.SweepOrphans.
	Delete(ctx context.Context, b Bucket, k Key) error
	// List calls fn for every object in b whose key starts with prefix, in no order; fn's
	// error stops the walk and is returned. Consumer: files.SweepOrphans.
	List(ctx context.Context, b Bucket, prefix string, fn func(Entry) error) error
	// Ping proves the store is usable: disk creates the root if needed and writes and
	// removes a temp file; s3 puts ProbeKey, reads it back anonymously (Invalid wrapping
	// ErrPublic when that succeeds) and deletes it. Consumers: files.Ping, files.Start.
	Ping(ctx context.Context) error
	// Kind names the implementation.
	Kind() Kind
}

// ErrMissing means there is no object under that key. It is Unavailable, because on a read-only copy the object
// may arrive with the next sync. Check errors.Is(err, ErrMissing) before the kind.
var ErrMissing = errors.New("objects: no such object")

// ErrPublic means the store answered an anonymous read of ProbeKey. It is Invalid: the bucket policy is
// wrong, and every object in it is exposed. Check errors.Is(err, ErrPublic) before the kind.
var ErrPublic = errors.New("objects: bucket is publicly readable")

func fieldErr(op, field, code, format string, args ...any) error {
	v := validate.Errors{Op: op}
	v.Addf(field, code, format, args...)
	return v.Err() //nolint:wrapcheck // Err already carries Op and errs.Invalid
}
