// Package objectstest holds the in-memory objects.Store for unit tests, and Conformance, the
// suite every objects.Store passes.
//
//nolint:wrapcheck // every error returned here is built by objects/internal/shared with its kind, or is the context's or the caller's own
package objectstest

import (
	"bytes"
	"context"
	"errors"
	"io"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/zedgama3/bms/internal/core/objects"
	"github.com/zedgama3/bms/internal/core/objects/internal/shared"
	"github.com/zedgama3/bms/internal/lib/clock"
	"github.com/zedgama3/bms/internal/lib/errs"
)

type memKey struct {
	bucket objects.Bucket
	key    objects.Key
}

type memObject struct {
	data     []byte
	sum      [32]byte
	ctype    string
	modified time.Time
}

// Mem is an in-memory objects.Store. It validates like the real stores, keeps objects
// until Delete, and reports Modified from its clock (time.Now until SetClock). It is safe
// for concurrent use.
type Mem struct {
	mu   sync.Mutex
	objs map[memKey]*memObject
	now  func() time.Time
	// failures are errors the next calls answer, by operation name: see Fail.
	failures map[string]error
	kind     objects.Kind
}

var _ objects.Store = (*Mem)(nil)

// NewMem returns an empty in-memory store.
func NewMem() *Mem {
	return &Mem{objs: map[memKey]*memObject{}, now: time.Now, failures: map[string]error{}, kind: objects.Disk}
}

// SetClock makes the store stamp Modified from c, so a test controls object age.
func (m *Mem) SetClock(c clock.Clock) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.now = c.Now
}

// Fail makes every later call of the named operation ("put", "get", "head", "delete",
// "list", "ping") answer err unchanged, before it looks at its arguments; Fail(op, nil)
// clears it. For tests of callers' failure handling.
func (m *Mem) Fail(op string, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err == nil {
		delete(m.failures, op)
		return
	}
	m.failures[op] = err
}

// Has reports whether the object exists.
func (m *Mem) Has(b objects.Bucket, k objects.Key) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.objs[memKey{b, k}]
	return ok
}

// Keys lists the keys in a bucket, sorted.
func (m *Mem) Keys(b objects.Bucket) []objects.Key {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []objects.Key
	for k := range m.objs {
		if k.bucket == b {
			out = append(out, k.key)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// Bytes returns a copy of an object's bytes, and whether it exists.
func (m *Mem) Bytes(b objects.Bucket, k objects.Key) ([]byte, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	o, ok := m.objs[memKey{b, k}]
	if !ok {
		return nil, false
	}
	return bytes.Clone(o.data), true
}

// Corrupt replaces an object's stored checksum with sum, leaving its bytes, so a test can
// see a Mismatched object. It reports whether the object exists.
func (m *Mem) Corrupt(b objects.Bucket, k objects.Key, sum [32]byte) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	o, ok := m.objs[memKey{b, k}]
	if ok {
		o.sum = sum
	}
	return ok
}

func (m *Mem) failure(op string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.failures[op]
}

// SetKind changes the Kind the store reports (objects.Disk until then), so a test can make
// it differ from the store that holds a file.
func (m *Mem) SetKind(k objects.Kind) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.kind = k
}

// Kind answers objects.Disk unless SetKind changed it: Mem stands in for the default store.
func (m *Mem) Kind() objects.Kind {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.kind
}

// Put stores the object; see objects.Store.Put.
func (m *Mem) Put(ctx context.Context, in objects.PutInput) (objects.Info, error) {
	const op = "objectstest.Mem.Put"
	if err := m.failure("put"); err != nil {
		return objects.Info{}, err
	}
	if err := in.Validate(); err != nil {
		return objects.Info{}, errs.E(op, errs.Invalid, err)
	}
	if err := shared.CtxErr(ctx, op); err != nil {
		return objects.Info{}, err
	}
	if in.Body == nil {
		return objects.Info{}, shared.SizeError(op)
	}
	vr := shared.NewVerifier(ctx, in)
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, vr); err != nil {
		if vr.Short() {
			return objects.Info{}, shared.SizeError(op)
		}
		return objects.Info{}, shared.Unavailable(op, err)
	}
	if err := vr.Finish(op); err != nil {
		return objects.Info{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	k := memKey{in.Bucket, in.Key}
	if _, ok := m.objs[k]; ok {
		return objects.Info{}, errs.E(op, errs.Conflict, errors.New("the object exists"))
	}
	o := &memObject{data: buf.Bytes(), sum: in.SHA256, ctype: in.ContentType, modified: m.now().UTC()}
	m.objs[k] = o
	return o.info(in.Bucket, in.Key), nil
}

func (o *memObject) info(b objects.Bucket, k objects.Key) objects.Info {
	return objects.Info{Bucket: b, Key: k, Size: int64(len(o.data)), SHA256: o.sum, ContentType: o.ctype, Modified: o.modified}
}

type readCloser struct{ *bytes.Reader }

func (readCloser) Close() error { return nil }

// Get answers the object; see objects.Store.Get.
func (m *Mem) Get(ctx context.Context, b objects.Bucket, k objects.Key) (objects.Object, error) {
	const op = "objectstest.Mem.Get"
	if err := m.failure("get"); err != nil {
		return objects.Object{}, err
	}
	if err := checkBK(op, b, k); err != nil {
		return objects.Object{}, err
	}
	if err := shared.CtxErr(ctx, op); err != nil {
		return objects.Object{}, err
	}
	m.mu.Lock()
	o, ok := m.objs[memKey{b, k}]
	m.mu.Unlock()
	if !ok {
		return objects.Object{}, shared.Missing(op)
	}
	return objects.Object{Info: o.info(b, k), Body: readCloser{bytes.NewReader(o.data)}}, nil
}

// Head answers the object's Info; see objects.Store.Head.
func (m *Mem) Head(ctx context.Context, b objects.Bucket, k objects.Key) (objects.Info, error) {
	const op = "objectstest.Mem.Head"
	if err := m.failure("head"); err != nil {
		return objects.Info{}, err
	}
	if err := checkBK(op, b, k); err != nil {
		return objects.Info{}, err
	}
	if err := shared.CtxErr(ctx, op); err != nil {
		return objects.Info{}, err
	}
	m.mu.Lock()
	o, ok := m.objs[memKey{b, k}]
	m.mu.Unlock()
	if !ok {
		return objects.Info{}, shared.Missing(op)
	}
	return o.info(b, k), nil
}

// Delete removes the object; a missing one is not an error.
func (m *Mem) Delete(ctx context.Context, b objects.Bucket, k objects.Key) error {
	const op = "objectstest.Mem.Delete"
	if err := m.failure("delete"); err != nil {
		return err
	}
	if err := checkBK(op, b, k); err != nil {
		return err
	}
	if err := shared.CtxErr(ctx, op); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.objs, memKey{b, k})
	return nil
}

// List calls fn for every object of b under prefix, sorted by key (Mem happens to order;
// callers must not rely on it). fn runs without the store's lock held.
func (m *Mem) List(ctx context.Context, b objects.Bucket, prefix string, fn func(objects.Entry) error) error {
	const op = "objectstest.Mem.List"
	if err := m.failure("list"); err != nil {
		return err
	}
	if err := b.Validate(); err != nil {
		return errs.E(op, errs.Invalid, err)
	}
	if fn == nil {
		return errs.E(op, errs.Invalid, errors.New("nil callback"))
	}
	if err := shared.CtxErr(ctx, op); err != nil {
		return err
	}
	m.mu.Lock()
	var entries []objects.Entry
	for k, o := range m.objs {
		if k.bucket == b && strings.HasPrefix(string(k.key), prefix) {
			entries = append(entries, objects.Entry{Key: k.key, Size: int64(len(o.data)), Modified: o.modified})
		}
	}
	m.mu.Unlock()
	sort.Slice(entries, func(i, j int) bool { return entries[i].Key < entries[j].Key })
	for _, e := range entries {
		if err := shared.CtxErr(ctx, op); err != nil {
			return err
		}
		if err := fn(e); err != nil {
			return err
		}
	}
	return nil
}

// Ping always succeeds unless Fail("ping", err) was called.
func (m *Mem) Ping(ctx context.Context) error {
	if err := m.failure("ping"); err != nil {
		return err
	}
	return shared.CtxErr(ctx, "objectstest.Mem.Ping")
}

func checkBK(op string, b objects.Bucket, k objects.Key) error {
	if err := b.Validate(); err != nil {
		return errs.E(op, errs.Invalid, err)
	}
	if err := k.Validate(); err != nil {
		return errs.E(op, errs.Invalid, err)
	}
	return nil
}
