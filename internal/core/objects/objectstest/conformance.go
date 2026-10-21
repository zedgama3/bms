package objectstest

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/core/objects"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/validate"
)

// confTimeout bounds every call the suite makes, so a hung store fails the test.
const confTimeout = 30 * time.Second

// Conformance runs the objects.Store contract against the stores factory makes. factory is
// called once per sub-test and must return a store that is empty as far as the suite's
// buckets go (a fresh directory, a fresh prefix); the suite uses buckets "confa" and
// "confb" only. Every objects.Store passes it: Mem and disk in unit tests, s3 under the
// integration tag.
func Conformance(t *testing.T, factory func(t *testing.T) objects.Store) {
	t.Helper()
	for _, tc := range []struct {
		name string
		run  func(t *testing.T, s objects.Store)
	}{
		{"PutHeadGetDelete", confRoundTrip},
		{"PutTwice_ReturnsConflict", confPutTwice},
		{"PutShortRead_ReturnsInvalidKeepsNothing", confPutShort},
		{"PutLongRead_ReturnsInvalidKeepsNothing", confPutLong},
		{"PutBadChecksum_ReturnsInvalidKeepsNothing", confPutBadChecksum},
		{"BadKey_ReturnsInvalid", confBadNames},
		{"GetMissing_ReturnsSentinel", confMissing},
		{"DeleteMissing_IsNil", confDeleteMissing},
		{"BucketIsolation", confIsolation},
		{"ListPrefix", confList},
		{"ListCallbackError_StopsAndReturnsIt", confListStops},
		{"SeekAcrossLargeObject", confSeek},
		{"ConcurrentPuts", confConcurrent},
		{"ContextDone_ReturnsTimeoutOrCanceled", confContext},
		{"PingOnEmptyRoot", confPing},
		{"KeyWithSlashes", confSlashKeys},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Helper()
			tc.run(t, factory(t))
		})
	}
}

const (
	bucketA objects.Bucket = "confa"
	bucketB objects.Bucket = "confb"
)

func ctxT(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), confTimeout)
	t.Cleanup(cancel)
	return ctx
}

func input(b objects.Bucket, k objects.Key, data []byte) objects.PutInput {
	return objects.PutInput{
		Bucket: b, Key: k, Size: int64(len(data)), SHA256: sha256.Sum256(data),
		ContentType: "application/octet-stream", Body: bytes.NewReader(data),
	}
}

func mustPut(t *testing.T, s objects.Store, b objects.Bucket, k objects.Key, data []byte) objects.Info {
	t.Helper()
	info, err := s.Put(ctxT(t), input(b, k, data))
	if err != nil {
		t.Fatalf("Put(%s, %s): %v", b, k, err)
	}
	return info
}

func wantKind(t *testing.T, what string, err error, kind errs.Kind) {
	t.Helper()
	if !errs.Is(err, kind) {
		t.Fatalf("%s: err = %v (kind %v), want kind %v", what, err, errs.KindOf(err), kind)
	}
}

func wantField(t *testing.T, err error, field string) {
	t.Helper()
	for _, fe := range validate.Fields(err) {
		if fe.Field == field {
			return
		}
	}
	t.Fatalf("err = %v, want a FieldError on %q", err, field)
}

func wantAbsent(t *testing.T, s objects.Store, b objects.Bucket, k objects.Key) {
	t.Helper()
	if _, err := s.Head(ctxT(t), b, k); !errors.Is(err, objects.ErrMissing) {
		t.Fatalf("Head after a failed Put: err = %v, want ErrMissing (nothing kept)", err)
	}
}

func confRoundTrip(t *testing.T, s objects.Store) {
	data := []byte("the quick brown fox")
	put := mustPut(t, s, bucketA, "one", data)
	if put.Size != int64(len(data)) || put.SHA256 != sha256.Sum256(data) || put.Bucket != bucketA || put.Key != "one" {
		t.Errorf("Put info = %+v", put)
	}
	head, err := s.Head(ctxT(t), bucketA, "one")
	if err != nil {
		t.Fatalf("Head: %v", err)
	}
	if head.Size != int64(len(data)) || head.Modified.IsZero() {
		t.Errorf("Head info = %+v", head)
	}
	if head.SHA256 != ([32]byte{}) && head.SHA256 != sha256.Sum256(data) {
		t.Errorf("Head checksum = %x, want the put checksum or zero", head.SHA256)
	}
	obj, err := s.Get(ctxT(t), bucketA, "one")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	got, rerr := io.ReadAll(obj.Body)
	if cerr := obj.Body.Close(); cerr != nil || rerr != nil {
		t.Fatalf("read/close: %v / %v", rerr, cerr)
	}
	if !bytes.Equal(got, data) {
		t.Errorf("Get body = %q, want %q", got, data)
	}
	if err := s.Delete(ctxT(t), bucketA, "one"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	wantAbsent(t, s, bucketA, "one")
}

func confPutTwice(t *testing.T, s objects.Store) {
	mustPut(t, s, bucketA, "dup", []byte("first"))
	_, err := s.Put(ctxT(t), input(bucketA, "dup", []byte("second")))
	wantKind(t, "second Put", err, errs.Conflict)
	obj, gerr := s.Get(ctxT(t), bucketA, "dup")
	if gerr != nil {
		t.Fatalf("Get: %v", gerr)
	}
	got, rerr := io.ReadAll(obj.Body)
	if cerr := obj.Body.Close(); cerr != nil || rerr != nil {
		t.Fatalf("read/close: %v / %v", rerr, cerr)
	}
	if string(got) != "first" {
		t.Errorf("the first object was replaced: %q", got)
	}
}

func confPutShort(t *testing.T, s objects.Store) {
	data := []byte("0123456789")
	in := input(bucketA, "short", data)
	in.Size = int64(len(data)) + 5 // declares more than the body has
	_, err := s.Put(ctxT(t), in)
	wantKind(t, "short body", err, errs.Invalid)
	wantField(t, err, "size")
	wantAbsent(t, s, bucketA, "short")
}

func confPutLong(t *testing.T, s objects.Store) {
	data := []byte("0123456789")
	in := input(bucketA, "long", data)
	in.Size = int64(len(data)) - 3 // declares fewer than the body has
	in.SHA256 = sha256.Sum256(data[:in.Size])
	_, err := s.Put(ctxT(t), in)
	wantKind(t, "long body", err, errs.Invalid)
	wantField(t, err, "size")
	wantAbsent(t, s, bucketA, "long")
}

func confPutBadChecksum(t *testing.T, s objects.Store) {
	in := input(bucketA, "sum", []byte("some bytes"))
	in.SHA256 = sha256.Sum256([]byte("other bytes"))
	_, err := s.Put(ctxT(t), in)
	wantKind(t, "wrong checksum", err, errs.Invalid)
	wantField(t, err, "checksum")
	wantAbsent(t, s, bucketA, "sum")
	// A zero checksum never matches.
	in = input(bucketA, "sum", []byte("some bytes"))
	in.SHA256 = [32]byte{}
	_, err = s.Put(ctxT(t), in)
	wantKind(t, "zero checksum", err, errs.Invalid)
	wantField(t, err, "checksum")
}

func confBadNames(t *testing.T, s objects.Store) {
	ctx := ctxT(t)
	for _, k := range []objects.Key{"", "a//b", "../x", "a/./b", "/lead", "trail/", "sp ace", "ü", objects.Key(bytes.Repeat([]byte("k"), objects.MaxKeyBytes+1))} {
		_, err := s.Put(ctx, input(bucketA, k, []byte("x")))
		wantKind(t, fmt.Sprintf("Put key %q", k), err, errs.Invalid)
		_, err = s.Get(ctx, bucketA, k)
		wantKind(t, fmt.Sprintf("Get key %q", k), err, errs.Invalid)
		_, err = s.Head(ctx, bucketA, k)
		wantKind(t, fmt.Sprintf("Head key %q", k), err, errs.Invalid)
		wantKind(t, fmt.Sprintf("Delete key %q", k), s.Delete(ctx, bucketA, k), errs.Invalid)
	}
	for _, b := range []objects.Bucket{"", "Upper", "1num", "has-dash", objects.Bucket(bytes.Repeat([]byte("b"), objects.MaxBucketBytes+1))} {
		_, err := s.Put(ctx, input(b, "k", []byte("x")))
		wantKind(t, fmt.Sprintf("Put bucket %q", b), err, errs.Invalid)
		_, err = s.Get(ctx, b, "k")
		wantKind(t, fmt.Sprintf("Get bucket %q", b), err, errs.Invalid)
		wantKind(t, fmt.Sprintf("List bucket %q", b), s.List(ctx, b, "", func(objects.Entry) error { return nil }), errs.Invalid)
	}
}

func confMissing(t *testing.T, s objects.Store) {
	ctx := ctxT(t)
	_, err := s.Get(ctx, bucketA, "nothing")
	if !errors.Is(err, objects.ErrMissing) {
		t.Errorf("Get missing: err = %v, want ErrMissing", err)
	}
	wantKind(t, "Get missing", err, errs.Unavailable)
	_, err = s.Head(ctx, bucketA, "nothing")
	if !errors.Is(err, objects.ErrMissing) {
		t.Errorf("Head missing: err = %v, want ErrMissing", err)
	}
	wantKind(t, "Head missing", err, errs.Unavailable)
}

func confDeleteMissing(t *testing.T, s objects.Store) {
	if err := s.Delete(ctxT(t), bucketA, "never-was"); err != nil {
		t.Fatalf("Delete of a missing object: %v", err)
	}
	mustPut(t, s, bucketA, "twice", []byte("x"))
	for i := range 2 {
		if err := s.Delete(ctxT(t), bucketA, "twice"); err != nil {
			t.Fatalf("Delete #%d: %v", i+1, err)
		}
	}
}

func confIsolation(t *testing.T, s objects.Store) {
	mustPut(t, s, bucketA, "same", []byte("in a"))
	mustPut(t, s, bucketB, "same", []byte("in b"))
	for b, want := range map[objects.Bucket]string{bucketA: "in a", bucketB: "in b"} {
		obj, err := s.Get(ctxT(t), b, "same")
		if err != nil {
			t.Fatalf("Get %s: %v", b, err)
		}
		got, rerr := io.ReadAll(obj.Body)
		if cerr := obj.Body.Close(); cerr != nil || rerr != nil {
			t.Fatalf("read/close: %v / %v", rerr, cerr)
		}
		if string(got) != want {
			t.Errorf("bucket %s holds %q, want %q", b, got, want)
		}
	}
	if err := s.Delete(ctxT(t), bucketA, "same"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := s.Head(ctxT(t), bucketB, "same"); err != nil {
		t.Errorf("deleting in bucket a removed bucket b's object: %v", err)
	}
	var seen []objects.Key
	if err := s.List(ctxT(t), bucketA, "", func(e objects.Entry) error { seen = append(seen, e.Key); return nil }); err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(seen) != 0 {
		t.Errorf("List(a) = %v after its only object was deleted", seen)
	}
}

func listKeys(t *testing.T, s objects.Store, b objects.Bucket, prefix string) []objects.Key {
	t.Helper()
	var keys []objects.Key
	seen := map[objects.Key]bool{}
	err := s.List(ctxT(t), b, prefix, func(e objects.Entry) error {
		if seen[e.Key] {
			t.Errorf("List returned %s twice", e.Key)
		}
		seen[e.Key] = true
		if e.Modified.IsZero() {
			t.Errorf("List entry %s has no Modified", e.Key)
		}
		keys = append(keys, e.Key)
		return nil
	})
	if err != nil {
		t.Fatalf("List(%s, %q): %v", b, prefix, err)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	return keys
}

func confList(t *testing.T, s objects.Store) {
	if got := listKeys(t, s, bucketA, ""); len(got) != 0 {
		t.Fatalf("List of an empty bucket = %v", got)
	}
	for _, k := range []objects.Key{"alpha", "alpine", "beta", "dir/one", "dir/two"} {
		mustPut(t, s, bucketA, k, []byte("x"))
	}
	for _, tc := range []struct {
		prefix string
		want   []objects.Key
	}{
		{"", []objects.Key{"alpha", "alpine", "beta", "dir/one", "dir/two"}},
		{"alp", []objects.Key{"alpha", "alpine"}},
		{"alph", []objects.Key{"alpha"}},
		{"dir/", []objects.Key{"dir/one", "dir/two"}},
		{"zzz", nil},
	} {
		got := listKeys(t, s, bucketA, tc.prefix)
		if fmt.Sprint(got) != fmt.Sprint(tc.want) {
			t.Errorf("List prefix %q = %v, want %v", tc.prefix, got, tc.want)
		}
	}
}

func confListStops(t *testing.T, s objects.Store) {
	for _, k := range []objects.Key{"a", "b", "c"} {
		mustPut(t, s, bucketA, k, []byte("x"))
	}
	boom := errors.New("stop here")
	calls := 0
	err := s.List(ctxT(t), bucketA, "", func(objects.Entry) error { calls++; return boom })
	if !errors.Is(err, boom) {
		t.Fatalf("List = %v, want the callback's error", err)
	}
	if calls != 1 {
		t.Errorf("callback ran %d times after it failed", calls)
	}
}

func confSeek(t *testing.T, s objects.Store) {
	const size = 3<<20 + 512<<10 // 3.5 MiB
	data := make([]byte, size)
	for i := range data {
		data[i] = byte(i*7 + i>>8)
	}
	mustPut(t, s, bucketA, "big", data)
	obj, err := s.Get(ctxT(t), bucketA, "big")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	defer func() {
		if cerr := obj.Body.Close(); cerr != nil {
			t.Errorf("Close: %v", cerr)
		}
	}()
	if obj.Size != size {
		t.Errorf("Size = %d, want %d", obj.Size, size)
	}
	for _, at := range []int64{0, 5, 1 << 20, 2<<20 + 17, size - 100} {
		if _, err := obj.Body.Seek(at, io.SeekStart); err != nil {
			t.Fatalf("Seek(%d): %v", at, err)
		}
		buf := make([]byte, 64)
		n, err := io.ReadFull(obj.Body, buf)
		if err != nil {
			t.Fatalf("read at %d: %v", at, err)
		}
		if !bytes.Equal(buf[:n], data[at:at+int64(n)]) {
			t.Errorf("bytes at %d differ", at)
		}
	}
	end, err := obj.Body.Seek(0, io.SeekEnd)
	if err != nil || end != size {
		t.Errorf("Seek end = %d, %v; want %d", end, err, int64(size))
	}
	if _, err := obj.Body.Seek(-10, io.SeekEnd); err != nil {
		t.Fatalf("Seek(-10, end): %v", err)
	}
	tail, err := io.ReadAll(obj.Body)
	if err != nil || !bytes.Equal(tail, data[size-10:]) {
		t.Errorf("tail = %v, %v", tail, err)
	}
}

func confConcurrent(t *testing.T, s objects.Store) {
	const n = 12
	var wg sync.WaitGroup
	errsCh := make(chan error, n)
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			data := []byte(fmt.Sprintf("object number %d", i))
			_, err := s.Put(ctxT(t), input(bucketA, objects.Key(fmt.Sprintf("c%02d", i)), data))
			errsCh <- err
		}()
	}
	wg.Wait()
	close(errsCh)
	for err := range errsCh {
		if err != nil {
			t.Errorf("concurrent Put: %v", err)
		}
	}
	if got := len(listKeys(t, s, bucketA, "c")); got != n {
		t.Errorf("List after %d concurrent Puts = %d keys", n, got)
	}
	// The same key from several goroutines: exactly one wins, the rest are Conflict.
	var winners, conflicts int
	var mu sync.Mutex
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := s.Put(ctxT(t), input(bucketA, "race", []byte(fmt.Sprintf("racer %d", i))))
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				winners++
			case errs.Is(err, errs.Conflict):
				conflicts++
			default:
				t.Errorf("racing Put: %v", err)
			}
		}()
	}
	wg.Wait()
	if winners != 1 || conflicts != n-1 {
		t.Errorf("racing Puts: %d winners and %d conflicts, want 1 and %d", winners, conflicts, n-1)
	}
}

func confContext(t *testing.T, s objects.Store) {
	mustPut(t, s, bucketA, "ctx", []byte("x"))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := s.Get(ctx, bucketA, "ctx")
	wantKind(t, "Get with a canceled ctx", err, errs.Canceled)
	_, err = s.Put(ctx, input(bucketA, "ctx2", []byte("x")))
	wantKind(t, "Put with a canceled ctx", err, errs.Canceled)
	dctx, dcancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer dcancel()
	wantKind(t, "Delete with an expired ctx", s.Delete(dctx, bucketA, "ctx"), errs.Timeout)
	wantKind(t, "Ping with an expired ctx", s.Ping(dctx), errs.Timeout)
	if _, err := s.Head(ctxT(t), bucketA, "ctx"); err != nil {
		t.Errorf("an operation under a dead ctx removed the object: %v", err)
	}
}

func confPing(t *testing.T, s objects.Store) {
	if err := s.Ping(ctxT(t)); err != nil {
		t.Fatalf("Ping on an empty store: %v", err)
	}
	if err := s.Ping(ctxT(t)); err != nil {
		t.Fatalf("second Ping: %v", err)
	}
	if got := listKeys(t, s, bucketA, ""); len(got) != 0 {
		t.Errorf("Ping left objects behind: %v", got)
	}
	if k := s.Kind(); k != objects.Disk && k != objects.S3 {
		t.Errorf("Kind = %v", k)
	}
}

func confSlashKeys(t *testing.T, s objects.Store) {
	data := []byte("nested")
	mustPut(t, s, bucketA, "a/b/c.txt", data)
	info, err := s.Head(ctxT(t), bucketA, "a/b/c.txt")
	if err != nil || info.Size != int64(len(data)) {
		t.Fatalf("Head: %+v, %v", info, err)
	}
	if got := listKeys(t, s, bucketA, "a/b/"); fmt.Sprint(got) != "[a/b/c.txt]" {
		t.Errorf("List = %v", got)
	}
	if err := s.Delete(ctxT(t), bucketA, "a/b/c.txt"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
}
