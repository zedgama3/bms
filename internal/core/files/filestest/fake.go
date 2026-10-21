// Package filestest gives tests an in-memory files.Store. moduletest hands it to a module as
// Deps.Files, so a module's own tests need no database and no object store.
package filestest

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/zedgama3/bms/internal/core/audit"
	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/files"
	"github.com/zedgama3/bms/internal/core/objects"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/validate"
	"github.com/zedgama3/bms/internal/model"
)

type stored struct {
	file  files.File
	bytes []byte
}

// Fake is a files.Store for one owner that keeps everything in memory. It validates like the
// real service (the Upload, Attachment, Staged and record-scope rules, the transaction
// rules, the default Policy) but strips no metadata and audits nothing; its transactions
// cannot roll back, so a test that rolls back sees the file anyway. Safe for concurrent use.
type Fake struct {
	owner string

	mu      sync.Mutex
	files   map[files.FileID]*stored
	staged  map[objects.Key]stagedObj
	failure error
}

type stagedObj struct {
	st    files.Staged
	bytes []byte
}

var _ files.Store = (*Fake)(nil)

// NewFake returns an empty Fake for owner (a module name or core area).
func NewFake(owner string) *Fake {
	return &Fake{owner: owner, files: map[files.FileID]*stored{}, staged: map[objects.Key]stagedObj{}}
}

// Fail makes every later call answer err; Fail(nil) clears it.
func (f *Fake) Fail(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failure = err
}

// Files returns what was attached and not deleted, oldest first.
func (f *Fake) Files() []files.File {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]files.File, 0, len(f.files))
	for _, s := range f.files {
		out = append(out, s.file)
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].CreatedAt.Before(out[j].CreatedAt)
		}
		return out[i].ID.String() < out[j].ID.String()
	})
	return out
}

// Staged returns what was staged and neither attached nor discarded.
func (f *Fake) Staged() []files.Staged {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]files.Staged, 0, len(f.staged))
	for _, s := range f.staged {
		out = append(out, s.st)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

func (f *Fake) fail() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.failure
}

func field(op string, kind errs.Kind, name, code string, params map[string]string, reason string) error {
	return errs.E(op, kind, &validate.FieldError{Field: name, Code: code, Params: params, Reason: reason})
}

func area(kind string) string {
	if i := strings.IndexByte(kind, '.'); i >= 0 {
		return kind[:i]
	}
	return ""
}

func (f *Fake) checkRecord(op string, rec audit.Ref) error {
	switch {
	case rec.IsZero():
		return field(op, errs.Invalid, "record", validate.CodeRequired, nil, "must be set")
	case rec.Validate() != nil:
		return field(op, errs.Invalid, "record", validate.CodeMalformed, nil, "must be <area>.<ident> with an id")
	case area(rec.Kind) != f.owner:
		return field(op, errs.Invalid, "record", "scope", nil, "must belong to the owner's area")
	}
	return nil
}

func needWrite(ctx context.Context, op string) error {
	if mode, in := db.TxMode(ctx); !in || mode != db.Write {
		return errs.E(op, errs.Internal, errors.New("called outside a Write transaction"))
	}
	return nil
}

// Stage keeps the bytes in memory after the real Stage's checks: not inside a transaction,
// a valid Upload, a non-empty body within the default policy's size, a sniffed type that
// Accept allows.
func (f *Fake) Stage(ctx context.Context, up files.Upload) (files.Staged, error) {
	const op = "filestest.Fake.Stage"
	if err := f.fail(); err != nil {
		return files.Staged{}, err
	}
	if _, in := db.TxMode(ctx); in {
		return files.Staged{}, errs.E(op, errs.Internal, errors.New("Stage called inside a transaction"))
	}
	if err := up.Validate(); err != nil {
		return files.Staged{}, errs.E(op, errs.Invalid, err)
	}
	if up.Body == nil {
		return files.Staged{}, field(op, errs.Invalid, "file", validate.CodeRequired, nil, "choose a file")
	}
	pol := files.DefaultPolicy()
	data, err := io.ReadAll(io.LimitReader(up.Body, pol.MaxBytes+1))
	if err != nil {
		var classified *errs.Error
		if errors.As(err, &classified) { // like the real Stage: a classified body error is returned unchanged
			return files.Staged{}, err //nolint:wrapcheck // the contract: a classified body error comes back unchanged
		}
		return files.Staged{}, errs.E(op, errs.Invalid, fmt.Errorf("read the body: %w", err))
	}
	if len(data) == 0 {
		return files.Staged{}, field(op, errs.Invalid, "file", validate.CodeRequired, nil, "choose a file")
	}
	if int64(len(data)) > pol.MaxBytes {
		return files.Staged{}, field(op, errs.Invalid, "file", "too_large", map[string]string{"max": strconv.FormatInt(pol.MaxBytes, 10)}, "too large")
	}
	head := data
	if len(head) > files.SniffBytes {
		head = head[:files.SniffBytes]
	}
	kind, ok := files.Detect(head)
	if !ok || !up.Accept.Has(kind) {
		return files.Staged{}, field(op, errs.Invalid, "file", "type", nil, "this kind of file is not accepted")
	}
	id := model.NewID[files.FileID]()
	st := files.Staged{
		Bucket: objects.Bucket(f.owner), Key: objects.Key(id.String()), Name: storedName(up.Name, kind),
		ContentType: kind.ContentType(), Size: int64(len(data)), SHA256: sha256.Sum256(data),
		Stripped: kind != files.PDF && !up.KeepMetadata, StagedAt: time.Now().UTC(),
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.staged[st.Key] = stagedObj{st: st, bytes: data}
	return st, nil
}

// storedName mirrors the real service: base name, extension forced to the kind's.
func storedName(raw string, k files.Kind) string {
	name := strings.ReplaceAll(raw, "\\", "/")
	if i := strings.LastIndex(name, "/"); i >= 0 {
		name = name[i+1:]
	}
	name = strings.TrimSpace(name)
	if name == "." || name == ".." {
		name = ""
	}
	stem := name
	if i := strings.LastIndex(name, "."); i >= 0 {
		stem = name[:i]
	}
	if stem == "" {
		stem = "file"
	}
	return stem + k.Extension()
}

// Attach binds a staged object to a record. It needs a Write transaction (errs.Internal
// otherwise), a record in the owner's area, a Staged this Fake made (not older than
// OrphanAge), and respects the default policy's per-record limit.
func (f *Fake) Attach(ctx context.Context, st files.Staged, a files.Attachment) (files.File, error) {
	const op = "filestest.Fake.Attach"
	if err := f.fail(); err != nil {
		return files.File{}, err
	}
	if err := a.Validate(); err != nil {
		return files.File{}, errs.E(op, errs.Invalid, err)
	}
	if err := f.checkRecord(op, a.Record); err != nil {
		return files.File{}, err
	}
	if err := st.Validate(); err != nil {
		return files.File{}, errs.E(op, errs.Invalid, err)
	}
	if string(st.Bucket) != f.owner {
		return files.File{}, field(op, errs.Invalid, "staged", "foreign", nil, "was staged for another owner")
	}
	if time.Since(st.StagedAt) > files.OrphanAge {
		return files.File{}, field(op, errs.Invalid, "staged", "expired", nil, "was staged too long ago")
	}
	if err := needWrite(ctx, op); err != nil {
		return files.File{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	so, ok := f.staged[st.Key]
	if !ok {
		return files.File{}, errs.E(op, errs.Internal, errors.New("the Staged object is not staged here (used twice?)"))
	}
	if a.Replace {
		f.deleteRecordLocked(a.Record)
	}
	n := 0
	for _, s := range f.files {
		if s.file.Record == a.Record {
			n++
		}
	}
	if limit := files.DefaultPolicy().MaxPerRecord; n >= limit {
		return files.File{}, field(op, errs.Conflict, "file", "too_many", map[string]string{"max": strconv.Itoa(limit)}, "the record holds as many files as it may")
	}
	id, err := model.ParseID[files.FileID](string(st.Key))
	if err != nil {
		return files.File{}, field(op, errs.Invalid, "staged", validate.CodeMalformed, nil, "is not an object Stage produced")
	}
	file := files.File{
		ID: id, Record: a.Record, Subject: a.Subject, Level: a.Level, Name: so.st.Name,
		ContentType: so.st.ContentType, Size: so.st.Size, SHA256: so.st.SHA256, Stripped: so.st.Stripped,
		CreatedAt: time.Now().UTC(),
	}
	delete(f.staged, st.Key)
	f.files[id] = &stored{file: file, bytes: so.bytes}
	return file, nil
}

// Discard forgets a staged object; a no-op for one that is gone.
func (f *Fake) Discard(_ context.Context, st files.Staged) error {
	const op = "filestest.Fake.Discard"
	if err := f.fail(); err != nil {
		return err
	}
	if err := st.Validate(); err != nil {
		return errs.E(op, errs.Invalid, err)
	}
	if string(st.Bucket) != f.owner {
		return field(op, errs.Invalid, "staged", "foreign", nil, "was staged for another owner")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.staged, st.Key)
	return nil
}

// Get answers one file's metadata or errs.NotFound.
func (f *Fake) Get(_ context.Context, id files.FileID) (files.File, error) {
	if err := f.fail(); err != nil {
		return files.File{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	s, ok := f.files[id]
	if !ok {
		return files.File{}, errs.E("filestest.Fake.Get", errs.NotFound, errors.New("no such file"))
	}
	return s.file, nil
}

// ByRecord lists a record's files, oldest first.
func (f *Fake) ByRecord(_ context.Context, rec audit.Ref) ([]files.File, error) {
	const op = "filestest.Fake.ByRecord"
	if err := f.fail(); err != nil {
		return nil, err
	}
	if err := f.checkRecord(op, rec); err != nil {
		return nil, err
	}
	out := []files.File{}
	for _, file := range f.Files() {
		if file.Record == rec {
			out = append(out, file)
		}
	}
	return out, nil
}

type nopCloser struct{ *bytes.Reader }

func (nopCloser) Close() error { return nil }

// Open answers the metadata and the bytes; it audits nothing and needs no actor.
func (f *Fake) Open(_ context.Context, id files.FileID) (files.File, files.Content, error) {
	if err := f.fail(); err != nil {
		return files.File{}, nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	s, ok := f.files[id]
	if !ok {
		return files.File{}, nil, errs.E("filestest.Fake.Open", errs.NotFound, errors.New("no such file"))
	}
	return s.file, nopCloser{bytes.NewReader(s.bytes)}, nil
}

// Delete removes one file; it needs a Write transaction.
func (f *Fake) Delete(ctx context.Context, id files.FileID) error {
	const op = "filestest.Fake.Delete"
	if err := f.fail(); err != nil {
		return err
	}
	if err := needWrite(ctx, op); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.files[id]; !ok {
		return errs.E(op, errs.NotFound, errors.New("no such file"))
	}
	delete(f.files, id)
	return nil
}

// DeleteByRecord removes a record's files and counts them; it needs a Write transaction.
func (f *Fake) DeleteByRecord(ctx context.Context, rec audit.Ref) (int, error) {
	const op = "filestest.Fake.DeleteByRecord"
	if err := f.fail(); err != nil {
		return 0, err
	}
	if err := needWrite(ctx, op); err != nil {
		return 0, err
	}
	if err := f.checkRecord(op, rec); err != nil {
		return 0, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.deleteRecordLocked(rec), nil
}

func (f *Fake) deleteRecordLocked(rec audit.Ref) int {
	n := 0
	for id, s := range f.files {
		if s.file.Record == rec {
			delete(f.files, id)
			n++
		}
	}
	return n
}

// DeletePerson applies what the real files DeletePerson applies: the uploader of the
// person's uploads is cleared (counted as Anonymized), and a file that still has the person
// as its subject is errs.Conflict (code in_use), because the owner forgot to delete it.
func (f *Fake) DeletePerson(ctx context.Context, person model.PersonID) (files.Deletion, error) {
	const op = "filestest.Fake.DeletePerson"
	if err := f.fail(); err != nil {
		return files.Deletion{}, err
	}
	if person.IsZero() {
		return files.Deletion{}, field(op, errs.Invalid, "person", validate.CodeRequired, nil, "must be set")
	}
	if err := needWrite(ctx, op); err != nil {
		return files.Deletion{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	n, left := 0, 0
	for _, s := range f.files {
		if s.file.UploadedBy == person {
			s.file.UploadedBy = model.PersonID{}
			n++
		}
		if s.file.Subject == person {
			left++
		}
	}
	if left > 0 {
		return files.Deletion{}, field(op, errs.Conflict, "files", "in_use", nil,
			fmt.Sprintf("files about the person remain (%s: %d)", f.owner, left))
	}
	return files.Deletion{Anonymized: n}, nil
}
