//nolint:wrapcheck // test code: errors come straight from the fake or store under test; fixtures do bounded arithmetic
package files_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"github.com/zedgama3/bms/internal/core/jobs/jobstest"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5" //nolint:depguard // attachedDB must implement db.DB, whose QueryRow returns pgx.Row
	"github.com/zedgama3/bms/internal/core/audit"
	"github.com/zedgama3/bms/internal/core/audit/audittest"
	"github.com/zedgama3/bms/internal/core/authz"
	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/db/dbtest"
	"github.com/zedgama3/bms/internal/core/files"
	"github.com/zedgama3/bms/internal/core/objects"
	"github.com/zedgama3/bms/internal/core/objects/objectstest"
	"github.com/zedgama3/bms/internal/core/settings"
	"github.com/zedgama3/bms/internal/core/settings/settingstest"
	"github.com/zedgama3/bms/internal/core/telemetry/telemetrytest"
	"github.com/zedgama3/bms/internal/lib/clock"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/secret"
	"github.com/zedgama3/bms/internal/lib/validate"
	"github.com/zedgama3/bms/internal/model"
)

// Two owners, as modules would be: files created through one are invisible to the other.
const (
	ownerVol = "volunteers"
	ownerInc = "incidents"

	kindQual  = "volunteers.qualification-record"
	kindPhoto = "incidents.photo"
)

// nameMarker is an invented name that must never reach a log, span, metric, error message,
// audit entry or object path. The file bytes carry it too, for the same reason.
const nameMarker = "Zorbl-Quillfeather-0773"

type (
	qualTag struct{}
	qualID  = model.ID[qualTag]
)

func qualRef() audit.Ref        { return audit.RefOf(kindQual, model.NewID[qualID]()) }
func newPerson() model.PersonID { return model.NewID[model.PersonID]() }

func ctxT(t testing.TB) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	t.Cleanup(cancel)
	return ctx
}

// ---- errors ----

func wantKind(t testing.TB, err error, kind errs.Kind) {
	t.Helper()
	if !errs.Is(err, kind) {
		t.Fatalf("error = %v (kind %v), want kind %v", err, errs.KindOf(err), kind)
	}
}

func wantNoErr(t testing.TB, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// wantField checks the kind and that a field error names field with code. The field may carry
// a prefix ("upload.file"), never a different leaf.
func fieldErr(t testing.TB, err error, kind errs.Kind, field, code string) *validate.FieldError {
	t.Helper()
	wantKind(t, err, kind)
	var seen []string
	for _, fe := range validate.Fields(err) {
		leaf := fe.Field
		if i := strings.LastIndex(leaf, "."); i >= 0 {
			leaf = leaf[i+1:]
		}
		if (leaf == field || fe.Field == field) && fe.Code == code {
			return fe
		}
		seen = append(seen, fe.Field+"/"+fe.Code)
	}
	t.Fatalf("no field error %s/%s in %v (found %v)", field, code, err, seen)
	return nil
}

// wantField is fieldErr for callers that do not need the field error.
func wantField(t testing.TB, err error, kind errs.Kind, field, code string) {
	t.Helper()
	if fieldErr(t, err, kind, field, code) == nil {
		t.Fatal("unreachable")
	}
}

func wantReadOnly(t testing.TB, err error) {
	t.Helper()
	wantKind(t, err, errs.Unavailable)
	if !errors.Is(err, db.ErrReadOnly) {
		t.Fatalf("error %v does not wrap db.ErrReadOnly", err)
	}
}

// ---- actor ----

type actorKey struct{}

func actorFromCtx(ctx context.Context) (authz.Actor, bool) {
	a, ok := ctx.Value(actorKey{}).(authz.Actor)
	return a, ok
}

// ---- policy ----

// policyBox is a settings.Reader for the "files" area that tests change as a files.Policy
// (set) or make fail (err). Reads are answered from a settingstest.Fake loaded with the
// current policy, so files' own key mapping is what the tests exercise.
type policyBox struct {
	mu  sync.Mutex
	p   files.Policy
	err error
}

var _ settings.Reader = (*policyBox)(nil)

func (b *policyBox) set(p files.Policy) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.p = p
}

func (b *policyBox) fake() (*settingstest.Fake, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.err != nil {
		return nil, b.err
	}
	f := settingstest.NewFake("files", files.Settings)
	f.Set("max_bytes", strconv.FormatInt(b.p.MaxBytes, 10))
	f.Set("quota_bytes", strconv.FormatInt(b.p.QuotaBytes, 10))
	f.Set("max_per_record", strconv.Itoa(b.p.MaxPerRecord))
	return f, nil
}

func (b *policyBox) Bool(ctx context.Context, k string) (bool, error) {
	f, err := b.fake()
	if err != nil {
		return false, err
	}
	return f.Bool(ctx, k)
}

func (b *policyBox) Int(ctx context.Context, k string) (int64, error) {
	f, err := b.fake()
	if err != nil {
		return 0, err
	}
	return f.Int(ctx, k)
}

func (b *policyBox) Duration(ctx context.Context, k string) (time.Duration, error) {
	f, err := b.fake()
	if err != nil {
		return 0, err
	}
	return f.Duration(ctx, k)
}

func (b *policyBox) Text(ctx context.Context, k string) (string, error) {
	f, err := b.fake()
	if err != nil {
		return "", err
	}
	return f.Text(ctx, k)
}

func (b *policyBox) Secret(ctx context.Context, k string) (secret.Secret, error) {
	f, err := b.fake()
	if err != nil {
		return secret.Secret{}, err
	}
	return f.Secret(ctx, k)
}

// ---- a store that fails on demand ----

type faultStore struct {
	objects.Store
	mu       sync.Mutex
	failOps  map[string]error
	failKeys map[objects.Key]error // Delete only
	kind     objects.Kind          // 0: the inner store's
	calls    map[string]int
}

func newFault(inner objects.Store) *faultStore {
	return &faultStore{Store: inner, failOps: map[string]error{}, failKeys: map[objects.Key]error{}, calls: map[string]int{}}
}

func unavailable(op string) error {
	return errs.E("test.faultStore."+op, errs.Unavailable, errors.New("store is down"))
}

// down makes every operation fail Unavailable.
func (f *faultStore) down() {
	for _, op := range []string{"put", "get", "head", "delete", "list", "ping"} {
		f.fail(op, unavailable(op))
	}
}

func (f *faultStore) up() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failOps = map[string]error{}
	f.failKeys = map[objects.Key]error{}
}

func (f *faultStore) fail(op string, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err == nil {
		delete(f.failOps, op)
		return
	}
	f.failOps[op] = err
}

func (f *faultStore) failKey(k objects.Key, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failKeys[k] = err
}

func (f *faultStore) count(op string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls[op]
}

func (f *faultStore) check(op string, k objects.Key) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls[op]++
	if err, ok := f.failKeys[k]; ok && op == "delete" {
		return err
	}
	return f.failOps[op]
}

func (f *faultStore) Put(ctx context.Context, in objects.PutInput) (objects.Info, error) {
	if err := f.check("put", in.Key); err != nil {
		return objects.Info{}, err
	}
	return f.Store.Put(ctx, in) //nolint:wrapcheck // test double
}

func (f *faultStore) Get(ctx context.Context, b objects.Bucket, k objects.Key) (objects.Object, error) {
	if err := f.check("get", k); err != nil {
		return objects.Object{}, err
	}
	return f.Store.Get(ctx, b, k) //nolint:wrapcheck // test double
}

func (f *faultStore) Head(ctx context.Context, b objects.Bucket, k objects.Key) (objects.Info, error) {
	if err := f.check("head", k); err != nil {
		return objects.Info{}, err
	}
	return f.Store.Head(ctx, b, k) //nolint:wrapcheck // test double
}

func (f *faultStore) Delete(ctx context.Context, b objects.Bucket, k objects.Key) error {
	if err := f.check("delete", k); err != nil {
		return err
	}
	return f.Store.Delete(ctx, b, k) //nolint:wrapcheck // test double
}

func (f *faultStore) List(ctx context.Context, b objects.Bucket, prefix string, fn func(objects.Entry) error) error {
	if err := f.check("list", ""); err != nil {
		return err
	}
	return f.Store.List(ctx, b, prefix, fn) //nolint:wrapcheck // test double
}

func (f *faultStore) Ping(ctx context.Context) error {
	if err := f.check("ping", ""); err != nil {
		return err
	}
	return f.Store.Ping(ctx) //nolint:wrapcheck // test double
}

func (f *faultStore) Kind() objects.Kind {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.kind != 0 {
		return f.kind
	}
	return f.Store.Kind()
}

// ---- the rig ----

type rig struct {
	t      testing.TB
	svc    *files.Service
	vol    files.Store // Scoped("volunteers")
	inc    files.Store // Scoped("incidents")
	store  *faultStore
	inner  objects.Store
	audit  *audittest.Fake
	clk    *clock.Fake
	tmp    string
	cap    *telemetrytest.Capture
	db     db.DB
	fake   *dbtest.Fake // nil when the rig runs on a real database
	policy *policyBox
	opts   files.Options
}

type rigOption func(*rig, *files.Options)

func withTempDir(dir string) rigOption {
	return func(r *rig, o *files.Options) { r.tmp, o.TempDir = dir, dir }
}

func withPolicy(p files.Policy) rigOption { return func(r *rig, _ *files.Options) { r.policy.set(p) } }

// newRig builds a Service over an in-memory store, dbtest.NewFake (replace it with withDB for
// integration tests), audittest.NewFake("files"), a fake clock that starts at the real time
// (so object modification times and the clock agree) and telemetrytest.
func newRig(t testing.TB, opts ...rigOption) *rig {
	t.Helper()
	r := &rig{t: t, audit: audittest.NewFake("files"), cap: telemetrytest.New(t),
		clk:    clock.NewFake(time.Now().UTC().Truncate(time.Microsecond)),
		policy: &policyBox{p: files.DefaultPolicy()}}
	r.fake = dbtest.NewFake()
	r.db = r.fake
	r.tmp = filepath.Join(t.TempDir(), "tmp")
	if err := os.MkdirAll(r.tmp, 0o700); err != nil {
		t.Fatalf("mkdir tmp: %v", err)
	}
	r.inner = objectstest.NewMem()
	r.store = newFault(r.inner)
	o := files.Options{
		DB: r.db, Clock: r.clk, Logger: r.cap.Logger(), Tracer: r.cap.Tracer("files"), Meter: r.cap.Meter("files"),
		Audit: r.audit, Actor: actorFromCtx, Objects: r.store, TempDir: r.tmp, Settings: r.policy,
		Jobs: jobstest.NewFake("files"),
	}
	for _, f := range opts {
		f(r, &o)
	}
	o.DB = firstDB(o.DB, r.db)
	r.db = o.DB
	r.opts = o
	svc, err := files.New(o)
	if err != nil {
		t.Fatalf("files.New: %v", err)
	}
	r.svc = svc
	if r.vol, err = svc.Scoped(ownerVol); err != nil {
		t.Fatalf("Scoped(%s): %v", ownerVol, err)
	}
	if r.inc, err = svc.Scoped(ownerInc); err != nil {
		t.Fatalf("Scoped(%s): %v", ownerInc, err)
	}
	return r
}

func firstDB(a, b db.DB) db.DB {
	if a != nil {
		return a
	}
	return b
}

// inTx runs fn inside a Write transaction on the rig's database.
func (r *rig) inTx(ctx context.Context, fn func(ctx context.Context) error) error {
	return r.db.Transact(ctx, db.Write, fn)
}

// stageBytes stages body as name for st, accepting accept.
func stageBytes(ctx context.Context, st files.Store, name string, accept files.Accept, body []byte) (files.Staged, error) {
	return st.Stage(ctx, files.Upload{Name: name, Accept: accept, Body: bytes.NewReader(body)})
}

func mustStage(t testing.TB, st files.Store, name string, accept files.Accept, body []byte) files.Staged {
	t.Helper()
	s, err := stageBytes(ctxT(t), st, name, accept, body)
	if err != nil {
		t.Fatalf("Stage(%q): %v", name, err)
	}
	return s
}

// objectKeys lists a bucket's keys on the rig's underlying store.
func (r *rig) objectKeys(b objects.Bucket) []string {
	r.t.Helper()
	var keys []string
	err := r.inner.List(ctxT(r.t), b, "", func(e objects.Entry) error {
		keys = append(keys, string(e.Key))
		return nil
	})
	if err != nil {
		r.t.Fatalf("List(%s): %v", b, err)
	}
	return keys
}

func (r *rig) objectBytes(b objects.Bucket, k objects.Key) []byte {
	r.t.Helper()
	o, err := r.inner.Get(ctxT(r.t), b, k)
	if err != nil {
		r.t.Fatalf("Get(%s/%s): %v", b, k, err)
	}
	defer o.Body.Close() //nolint:errcheck,gosec // test
	data, err := io.ReadAll(o.Body)
	if err != nil {
		r.t.Fatalf("read: %v", err)
	}
	return data
}

func (r *rig) wantNoTempFiles() {
	r.t.Helper()
	ents, err := os.ReadDir(r.tmp)
	if err != nil {
		r.t.Fatalf("read temp dir: %v", err)
	}
	if len(ents) != 0 {
		var names []string
		for _, e := range ents {
			names = append(names, e.Name())
		}
		r.t.Errorf("temp dir not empty: %v", names)
	}
}

func (r *rig) wantNoObjects(b objects.Bucket) {
	r.t.Helper()
	if keys := r.objectKeys(b); len(keys) != 0 {
		r.t.Errorf("bucket %s holds %v, want nothing", b, keys)
	}
}

// entriesWith returns the recorded audit entries whose action is action.
func entriesWith(a *audittest.Fake, action string) []audit.Entry {
	var out []audit.Entry
	for _, e := range a.Recorded() {
		if string(e.Action) == action {
			out = append(out, e)
		}
	}
	return out
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

// infiniteBody yields a valid file head and then endless filler, counting what was read, so a
// service that does not stop at its limit is caught by the count (and the test deadline).
type infiniteBody struct {
	head []byte
	read int64
	pos  int
}

func (b *infiniteBody) Read(p []byte) (int, error) {
	n := 0
	for n < len(p) {
		if b.pos < len(b.head) {
			c := copy(p[n:], b.head[b.pos:])
			b.pos += c
			n += c
			continue
		}
		p[n] = 'x'
		n++
	}
	b.read += int64(n)
	return n, nil
}

// failingReader fails every read with err.
type failingReader struct{ err error }

func (r *failingReader) Read([]byte) (int, error) { return 0, r.err }

func putRaw(t testing.TB, s objects.Store, b objects.Bucket, k objects.Key, data []byte) {
	t.Helper()
	sum := sha256.Sum256(data)
	_, err := s.Put(ctxT(t), objects.PutInput{Bucket: b, Key: k, Size: int64(len(data)), SHA256: sum, ContentType: "application/octet-stream", Body: bytes.NewReader(data)})
	if err != nil {
		t.Fatalf("raw Put(%s/%s): %v", b, k, err)
	}
}

// ---- a database whose "is this object attached?" lookup is scripted ----

// attachedDB wraps a db.DB and answers the file_exists_by_key lookup (Discard's and the
// sweep's "is it attached?") with a scripted value, so the unit tests do not depend on the
// fake's QueryRow, which yields NotFound (and Discard keeps the object on NotFound). Every
// other statement goes to the wrapped database.
type attachedDB struct {
	db.DB
	attached bool
	err      error // when set, the lookup's Scan fails with it instead
}

type scriptedRow struct {
	attached bool
	err      error
}

func (s scriptedRow) Scan(dest ...any) error {
	if s.err != nil {
		return s.err
	}
	if len(dest) != 1 {
		return errors.New("scriptedRow: want one destination")
	}
	p, ok := dest[0].(*bool)
	if !ok {
		return errors.New("scriptedRow: want *bool")
	}
	*p = s.attached
	return nil
}

func (a attachedDB) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if strings.Contains(sql, "core.file") && strings.Contains(sql, "AS attached") {
		return scriptedRow{attached: a.attached, err: a.err}
	}
	return a.DB.QueryRow(ctx, sql, args...)
}

// withAttachedLookup scripts the attached lookup on top of the rig's fake database.
func withAttachedLookup(attached bool, err error) rigOption {
	return func(_ *rig, o *files.Options) {
		o.DB = attachedDB{DB: dbtest.NewFake(), attached: attached, err: err}
	}
}
