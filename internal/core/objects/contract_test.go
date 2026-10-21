//nolint:gosec,wrapcheck // test code: errors come straight from the fake or store under test; fixtures do bounded arithmetic
package objects_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"math/rand/v2"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/core/objects"
	"github.com/zedgama3/bms/internal/core/objects/disk"
	"github.com/zedgama3/bms/internal/core/objects/objectstest"
	"github.com/zedgama3/bms/internal/lib/errs"
)

// An independent statement of the objects.Store contract (files.md, "Method contracts" and
// failure modes F58-F63), written from the spec so that objectstest.Conformance is not the
// only judge of the stores it is meant to judge. It runs for the in-memory store and for
// disk; contract_s3_integration_test.go runs it for s3.
//
// Traceability for the spec's objects failure modes. files.md names them
// TestObjects_<Case>; they are the objectstest.Conformance subtests, run once per store
// (the parent test is the one calling Conformance, the subtest is the name after "/"):
//
//	F58 TestObjects_PutTwice_ReturnsConflict                    Conformance/PutTwice_ReturnsConflict (confPutTwice)
//	F59 TestObjects_PutShortRead_ReturnsInvalidKeepsNothing     Conformance/PutShortRead_ReturnsInvalidKeepsNothing (confPutShort), PutLongRead_... (confPutLong)
//	F59 TestObjects_PutBadChecksum_ReturnsInvalidKeepsNothing   Conformance/PutBadChecksum_ReturnsInvalidKeepsNothing (confPutBadChecksum)
//	F60 TestObjects_BadKey_ReturnsInvalid                       Conformance/BadKey_ReturnsInvalid (confBadNames)
//	F61 TestObjects_GetMissing_ReturnsSentinel                  Conformance/GetMissing_ReturnsSentinel (confMissing)

type factory func(t *testing.T) objects.Store

func memFactory(*testing.T) objects.Store { return objectstest.NewMem() }

func diskFactory(t *testing.T) objects.Store {
	t.Helper()
	s, err := disk.New(t.TempDir())
	if err != nil {
		t.Fatalf("disk.New: %v", err)
	}
	return s
}

func TestObjects_Contract(t *testing.T) {
	for name, f := range map[string]factory{"mem": memFactory, "disk": diskFactory} {
		t.Run(name, func(t *testing.T) { runContract(t, f) })
		t.Run(name+"/Conformance", func(t *testing.T) { objectstest.Conformance(t, f) })
	}
}

func testCtx(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func put(t *testing.T, s objects.Store, b objects.Bucket, k objects.Key, data []byte) objects.Info {
	t.Helper()
	info, err := s.Put(testCtx(t), objects.PutInput{
		Bucket: b, Key: k, Size: int64(len(data)), SHA256: sha256.Sum256(data),
		ContentType: "application/octet-stream", Body: bytes.NewReader(data),
	})
	if err != nil {
		t.Fatalf("Put(%s/%s): %v", b, k, err)
	}
	return info
}

func mustMissing(t *testing.T, s objects.Store, b objects.Bucket, k objects.Key) {
	t.Helper()
	if _, err := s.Head(testCtx(t), b, k); !errors.Is(err, objects.ErrMissing) || !errs.Is(err, errs.Unavailable) {
		t.Fatalf("Head(%s/%s) = %v, want Unavailable wrapping ErrMissing", b, k, err)
	}
}

func listKeys(t *testing.T, s objects.Store, b objects.Bucket, prefix string) []string {
	t.Helper()
	var keys []string
	err := s.List(testCtx(t), b, prefix, func(e objects.Entry) error {
		keys = append(keys, string(e.Key))
		return nil
	})
	if err != nil {
		t.Fatalf("List(%s, %q): %v", b, prefix, err)
	}
	sort.Strings(keys)
	return keys
}

func payload(n int, seed uint64) []byte {
	r := rand.New(rand.NewChaCha8([32]byte{byte(seed), byte(seed >> 8), 7}))
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(r.UintN(256))
	}
	return b
}

func runContract(t *testing.T, mk factory) {
	t.Run("PutHeadGet_RoundTrip", func(t *testing.T) {
		s := mk(t)
		data := payload(10_000, 1)
		sum := sha256.Sum256(data)
		before := time.Now()
		info, err := s.Put(testCtx(t), objects.PutInput{Bucket: "files", Key: "a/one.bin", Size: int64(len(data)), SHA256: sum, ContentType: "image/png", Body: bytes.NewReader(data)})
		if err != nil {
			t.Fatalf("Put: %v", err)
		}
		if info.Bucket != "files" || info.Key != "a/one.bin" || info.Size != int64(len(data)) || info.SHA256 != sum || info.ContentType != "image/png" {
			t.Errorf("Put Info = %+v", info)
		}
		head, err := s.Head(testCtx(t), "files", "a/one.bin")
		if err != nil {
			t.Fatalf("Head: %v", err)
		}
		if head.Size != int64(len(data)) || head.ContentType != "image/png" || head.Key != "a/one.bin" {
			t.Errorf("Head Info = %+v", head)
		}
		if head.SHA256 != ([32]byte{}) && head.SHA256 != sum {
			t.Errorf("Head SHA256 = %x, want zero or %x", head.SHA256, sum)
		}
		if head.Modified.IsZero() || head.Modified.Before(before.Add(-5*time.Minute)) || head.Modified.After(time.Now().Add(5*time.Minute)) {
			t.Errorf("Head Modified = %v, want about now", head.Modified)
		}
		obj, err := s.Get(testCtx(t), "files", "a/one.bin")
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		defer obj.Body.Close() //nolint:errcheck // test
		got, err := io.ReadAll(obj.Body)
		if err != nil || !bytes.Equal(got, data) {
			t.Errorf("Get body differs (err %v, %d bytes)", err, len(got))
		}
		if obj.Size != int64(len(data)) {
			t.Errorf("Get Size = %d", obj.Size)
		}
	})

	t.Run("PutTwice_ReturnsConflictAndKeepsFirst", func(t *testing.T) { // F58
		s := mk(t)
		first := payload(100, 2)
		put(t, s, "files", "k", first)
		second := payload(100, 3)
		_, err := s.Put(testCtx(t), objects.PutInput{Bucket: "files", Key: "k", Size: 100, SHA256: sha256.Sum256(second), Body: bytes.NewReader(second)})
		if !errs.Is(err, errs.Conflict) {
			t.Fatalf("second Put err = %v, want Conflict", err)
		}
		obj, err := s.Get(testCtx(t), "files", "k")
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		defer obj.Body.Close() //nolint:errcheck // test
		got := readAllT(t, obj.Body)
		if !bytes.Equal(got, first) {
			t.Error("a refused Put replaced the existing object")
		}
	})

	t.Run("PutBadBody_ReturnsInvalidKeepsNothing", func(t *testing.T) { // F59
		data := payload(5000, 4)
		sum := sha256.Sum256(data)
		wrong := sum
		wrong[0] ^= 0xff
		tests := []struct {
			name string
			in   objects.PutInput
		}{
			{"short body", objects.PutInput{Size: 5000, SHA256: sum, Body: bytes.NewReader(data[:4000])}},
			{"long body", objects.PutInput{Size: 4000, SHA256: sha256.Sum256(data[:4000]), Body: bytes.NewReader(data)}},
			{"wrong checksum", objects.PutInput{Size: 5000, SHA256: wrong, Body: bytes.NewReader(data)}},
			{"zero checksum", objects.PutInput{Size: 5000, Body: bytes.NewReader(data)}},
		}
		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				s := mk(t)
				in := tc.in
				in.Bucket, in.Key = "files", "bad"
				if _, err := s.Put(testCtx(t), in); !errs.Is(err, errs.Invalid) {
					t.Fatalf("Put err = %v, want Invalid", err)
				}
				mustMissing(t, s, "files", "bad")
				if keys := listKeys(t, s, "files", ""); len(keys) != 0 {
					t.Errorf("List after failed Put = %v, want none", keys)
				}
				// The key is free: a correct Put now succeeds.
				put(t, s, "files", "bad", data)
			})
		}
	})

	t.Run("PutBodyError_KeepsNothing", func(t *testing.T) {
		s := mk(t)
		boom := errors.New("client went away")
		body := io.MultiReader(bytes.NewReader(payload(100, 5)), &errReader{err: boom})
		_, err := s.Put(testCtx(t), objects.PutInput{Bucket: "files", Key: "k", Size: 1000, SHA256: sha256.Sum256(payload(1000, 5)), Body: body})
		if err == nil {
			t.Fatal("Put with a failing body succeeded")
		}
		mustMissing(t, s, "files", "k")
	})

	t.Run("BadBucketOrKey_ReturnsInvalid", func(t *testing.T) { // F60
		s := mk(t)
		put(t, s, "files", "ok", []byte("x"))
		ctx := testCtx(t)
		badKeys := []objects.Key{"", ".", "..", "a/../b", "a//b", "/a", "a b", objects.Key(strings.Repeat("k", objects.MaxKeyBytes+1))}
		for _, k := range badKeys {
			in := objects.PutInput{Bucket: "files", Key: k, Size: 1, SHA256: sha256.Sum256([]byte("x")), Body: strings.NewReader("x")}
			if _, err := s.Put(ctx, in); !errs.Is(err, errs.Invalid) {
				t.Errorf("Put key %q err = %v, want Invalid", k, err)
			}
			if _, err := s.Get(ctx, "files", k); !errs.Is(err, errs.Invalid) {
				t.Errorf("Get key %q err = %v, want Invalid", k, err)
			}
			if _, err := s.Head(ctx, "files", k); !errs.Is(err, errs.Invalid) {
				t.Errorf("Head key %q err = %v, want Invalid", k, err)
			}
			if err := s.Delete(ctx, "files", k); !errs.Is(err, errs.Invalid) {
				t.Errorf("Delete key %q err = %v, want Invalid", k, err)
			}
		}
		for _, b := range []objects.Bucket{"", "Files", "1files", "a-b", objects.Bucket("a" + strings.Repeat("b", objects.MaxBucketBytes))} {
			in := objects.PutInput{Bucket: b, Key: "k", Size: 1, SHA256: sha256.Sum256([]byte("x")), Body: strings.NewReader("x")}
			if _, err := s.Put(ctx, in); !errs.Is(err, errs.Invalid) {
				t.Errorf("Put bucket %q err = %v, want Invalid", b, err)
			}
			if _, err := s.Get(ctx, b, "ok"); !errs.Is(err, errs.Invalid) {
				t.Errorf("Get bucket %q err = %v, want Invalid", b, err)
			}
			if _, err := s.Head(ctx, b, "ok"); !errs.Is(err, errs.Invalid) {
				t.Errorf("Head bucket %q err = %v, want Invalid", b, err)
			}
			if err := s.Delete(ctx, b, "ok"); !errs.Is(err, errs.Invalid) {
				t.Errorf("Delete bucket %q err = %v, want Invalid", b, err)
			}
			if err := s.List(ctx, b, "", func(objects.Entry) error { return nil }); !errs.Is(err, errs.Invalid) {
				t.Errorf("List bucket %q err = %v, want Invalid", b, err)
			}
		}
		// Nothing escaped the bucket: the one good object is still the only one.
		if keys := listKeys(t, s, "files", ""); len(keys) != 1 || keys[0] != "ok" {
			t.Errorf("keys = %v, want [ok]", keys)
		}
	})

	t.Run("GetHeadMissing_ReturnsSentinel", func(t *testing.T) { // F61
		s := mk(t)
		_, err := s.Get(testCtx(t), "files", "nope")
		if !errors.Is(err, objects.ErrMissing) || !errs.Is(err, errs.Unavailable) {
			t.Errorf("Get err = %v, want Unavailable wrapping ErrMissing", err)
		}
		_, err = s.Head(testCtx(t), "files", "nope")
		if !errors.Is(err, objects.ErrMissing) || !errs.Is(err, errs.Unavailable) {
			t.Errorf("Head err = %v, want Unavailable wrapping ErrMissing", err)
		}
	})

	t.Run("Delete_IsIdempotent", func(t *testing.T) {
		s := mk(t)
		put(t, s, "files", "k", []byte("abc"))
		for i := range 3 {
			if err := s.Delete(testCtx(t), "files", "k"); err != nil {
				t.Fatalf("Delete #%d: %v", i, err)
			}
		}
		mustMissing(t, s, "files", "k")
		if err := s.Delete(testCtx(t), "files", "never-existed"); err != nil {
			t.Errorf("Delete of a missing key = %v, want nil", err)
		}
		put(t, s, "files", "k", []byte("again")) // the key is reusable after Delete
	})

	t.Run("Buckets_AreIsolated", func(t *testing.T) {
		s := mk(t)
		put(t, s, "alpha", "same", []byte("in alpha"))
		put(t, s, "beta", "same", []byte("in beta"))
		for b, want := range map[objects.Bucket]string{"alpha": "in alpha", "beta": "in beta"} {
			obj, err := s.Get(testCtx(t), b, "same")
			if err != nil {
				t.Fatalf("Get %s: %v", b, err)
			}
			got := readAllT(t, obj.Body)
			obj.Body.Close() //nolint:errcheck // test
			if string(got) != want {
				t.Errorf("bucket %s holds %q, want %q", b, got, want)
			}
		}
		if err := s.Delete(testCtx(t), "alpha", "same"); err != nil {
			t.Fatal(err)
		}
		mustMissing(t, s, "alpha", "same")
		if _, err := s.Head(testCtx(t), "beta", "same"); err != nil {
			t.Errorf("deleting in alpha removed beta's object: %v", err)
		}
		if keys := listKeys(t, s, "alpha", ""); len(keys) != 0 {
			t.Errorf("alpha keys = %v", keys)
		}
	})

	t.Run("List_PrefixAndEntries", func(t *testing.T) {
		s := mk(t)
		put(t, s, "files", "a/1", []byte("1"))
		put(t, s, "files", "a/22", []byte("22"))
		put(t, s, "files", "ab", []byte("333"))
		put(t, s, "files", "b/1", []byte("4444"))
		put(t, s, "other", "a/9", []byte("9"))
		cases := map[string][]string{
			"":     {"a/1", "a/22", "ab", "b/1"},
			"a/":   {"a/1", "a/22"},
			"a":    {"a/1", "a/22", "ab"},
			"b":    {"b/1"},
			"zzz":  nil,
			"a/22": {"a/22"},
		}
		for prefix, want := range cases {
			got := listKeys(t, s, "files", prefix)
			if strings.Join(got, ",") != strings.Join(want, ",") {
				t.Errorf("List prefix %q = %v, want %v", prefix, got, want)
			}
		}
		sizes := map[objects.Key]int64{}
		now := time.Now()
		err := s.List(testCtx(t), "files", "", func(e objects.Entry) error {
			sizes[e.Key] = e.Size
			if e.Modified.IsZero() || e.Modified.After(now.Add(5*time.Minute)) {
				t.Errorf("Entry %s Modified = %v", e.Key, e.Modified)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		if sizes["a/1"] != 1 || sizes["a/22"] != 2 || sizes["ab"] != 3 || sizes["b/1"] != 4 {
			t.Errorf("entry sizes = %v", sizes)
		}
		if keys := listKeys(t, s, "emptybucket", ""); len(keys) != 0 {
			t.Errorf("List of an empty bucket = %v, want none", keys)
		}
	})

	t.Run("List_CallbackErrorStopsAndIsReturned", func(t *testing.T) {
		s := mk(t)
		for _, k := range []objects.Key{"k1", "k2", "k3", "k4"} {
			put(t, s, "files", k, []byte("x"))
		}
		stop := errors.New("enough")
		calls := 0
		err := s.List(testCtx(t), "files", "", func(objects.Entry) error {
			calls++
			return stop
		})
		if !errors.Is(err, stop) {
			t.Errorf("List err = %v, want the callback's error", err)
		}
		if calls != 1 {
			t.Errorf("callback calls = %d, want 1", calls)
		}
	})

	t.Run("List_EachObjectOnce", func(t *testing.T) {
		s := mk(t)
		want := map[string]bool{}
		for i := range 60 {
			k := objects.Key("n/" + string(rune('a'+i%26)) + strings.Repeat("x", i/26) + "-" + time.Duration(i).String())
			put(t, s, "files", k, []byte{byte(i)})
			want[string(k)] = true
		}
		seen := map[string]int{}
		for _, k := range listKeys(t, s, "files", "n/") {
			seen[k]++
		}
		if len(seen) != len(want) {
			t.Fatalf("listed %d distinct keys, want %d", len(seen), len(want))
		}
		for k, n := range seen {
			if n != 1 || !want[k] {
				t.Errorf("key %q listed %d times (known %v)", k, n, want[k])
			}
		}
	})

	t.Run("Get_SeekAcrossLargeObject", func(t *testing.T) {
		s := mk(t)
		data := payload(3*1024*1024+512*1024, 6) // 3.5 MiB
		put(t, s, "files", "big", data)
		obj, err := s.Get(testCtx(t), "files", "big")
		if err != nil {
			t.Fatal(err)
		}
		defer obj.Body.Close() //nolint:errcheck // test
		read := func(n int) []byte {
			b := make([]byte, n)
			if _, err := io.ReadFull(obj.Body, b); err != nil {
				t.Fatalf("ReadFull: %v", err)
			}
			return b
		}
		off := int64(1024*1024 + 3)
		if pos, err := obj.Body.Seek(off, io.SeekStart); err != nil || pos != off {
			t.Fatalf("Seek start = %d, %v", pos, err)
		}
		if !bytes.Equal(read(4096), data[off:off+4096]) {
			t.Error("bytes after SeekStart differ")
		}
		if pos, err := obj.Body.Seek(-100, io.SeekEnd); err != nil || pos != int64(len(data))-100 {
			t.Fatalf("Seek end = %d, %v", pos, err)
		}
		if !bytes.Equal(read(100), data[len(data)-100:]) {
			t.Error("bytes after SeekEnd differ")
		}
		if pos, err := obj.Body.Seek(0, io.SeekCurrent); err != nil || pos != int64(len(data)) {
			t.Errorf("Seek current at end = %d, %v", pos, err)
		}
		if pos, err := obj.Body.Seek(10, io.SeekStart); err != nil || pos != 10 {
			t.Fatalf("Seek back = %d, %v", pos, err)
		}
		if pos, err := obj.Body.Seek(5, io.SeekCurrent); err != nil || pos != 15 {
			t.Fatalf("Seek current = %d, %v", pos, err)
		}
		if !bytes.Equal(read(64), data[15:15+64]) {
			t.Error("bytes after SeekCurrent differ")
		}
		if _, err := obj.Body.Seek(-1, io.SeekStart); err == nil {
			t.Error("Seek to a negative offset succeeded")
		}
	})

	t.Run("ConcurrentPuts_DistinctKeys", func(t *testing.T) {
		s := mk(t)
		const n = 24
		var wg sync.WaitGroup
		errc := make(chan error, n)
		for i := range n {
			wg.Add(1)
			go func() {
				defer wg.Done()
				data := payload(2000+i, uint64(i))
				_, err := s.Put(testCtx(t), objects.PutInput{Bucket: "files", Key: objects.Key("c" + time.Duration(i).String()), Size: int64(len(data)), SHA256: sha256.Sum256(data), Body: bytes.NewReader(data)})
				errc <- err
			}()
		}
		wg.Wait()
		close(errc)
		for err := range errc {
			if err != nil {
				t.Errorf("concurrent Put: %v", err)
			}
		}
		if keys := listKeys(t, s, "files", "c"); len(keys) != n {
			t.Errorf("listed %d keys, want %d", len(keys), n)
		}
	})

	t.Run("ConcurrentPuts_SameKey_ExactlyOneWins", func(t *testing.T) {
		s := mk(t)
		const n = 12
		data := payload(4096, 9)
		var wg sync.WaitGroup
		results := make(chan error, n)
		for range n {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, err := s.Put(testCtx(t), objects.PutInput{Bucket: "files", Key: "race", Size: int64(len(data)), SHA256: sha256.Sum256(data), Body: bytes.NewReader(data)})
				results <- err
			}()
		}
		wg.Wait()
		close(results)
		wins, conflicts := 0, 0
		for err := range results {
			switch {
			case err == nil:
				wins++
			case errs.Is(err, errs.Conflict):
				conflicts++
			default:
				t.Errorf("unexpected error: %v", err)
			}
		}
		if wins != 1 || conflicts != n-1 {
			t.Errorf("wins=%d conflicts=%d, want 1 and %d", wins, conflicts, n-1)
		}
		obj, err := s.Get(testCtx(t), "files", "race")
		if err != nil {
			t.Fatal(err)
		}
		defer obj.Body.Close() //nolint:errcheck // test
		if got := readAllT(t, obj.Body); !bytes.Equal(got, data) {
			t.Error("the surviving object is damaged")
		}
	})

	t.Run("ConcurrentGets", func(t *testing.T) {
		s := mk(t)
		data := payload(100_000, 10)
		put(t, s, "files", "shared", data)
		var wg sync.WaitGroup
		for range 16 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				obj, err := s.Get(testCtx(t), "files", "shared")
				if err != nil {
					t.Errorf("Get: %v", err)
					return
				}
				defer obj.Body.Close() //nolint:errcheck // test
				if got, err := io.ReadAll(obj.Body); err != nil || !bytes.Equal(got, data) {
					t.Errorf("concurrent read differs (err %v)", err)
				}
			}()
		}
		wg.Wait()
	})

	t.Run("Ping_OnEmptyStore_IsNilAndLeavesNothing", func(t *testing.T) {
		s := mk(t)
		if err := s.Ping(testCtx(t)); err != nil {
			t.Fatalf("Ping: %v", err)
		}
		if err := s.Ping(testCtx(t)); err != nil {
			t.Fatalf("second Ping: %v", err)
		}
		if keys := listKeys(t, s, "files", ""); len(keys) != 0 {
			t.Errorf("Ping left objects: %v", keys)
		}
	})

	t.Run("Kind_IsDefined", func(t *testing.T) {
		if k := mk(t).Kind(); k != objects.Disk && k != objects.S3 {
			t.Errorf("Kind = %v, want Disk or S3", k)
		}
	})

	t.Run("ContextDone_ReturnsCanceledOrTimeout", func(t *testing.T) { // F63
		s := mk(t)
		put(t, s, "files", "k", []byte("x"))
		canceled, cancel := context.WithCancel(t.Context())
		cancel()
		expired, cancel2 := context.WithDeadline(t.Context(), time.Now().Add(-time.Second))
		defer cancel2()
		for name, tc := range map[string]struct {
			ctx  context.Context
			kind errs.Kind
		}{"canceled": {canceled, errs.Canceled}, "expired": {expired, errs.Timeout}} {
			ops := map[string]func() error{
				"Put": func() error {
					_, err := s.Put(tc.ctx, objects.PutInput{Bucket: "files", Key: "new", Size: 1, SHA256: sha256.Sum256([]byte("y")), Body: strings.NewReader("y")})
					return err
				},
				"Get": func() error {
					o, err := s.Get(tc.ctx, "files", "k")
					if err == nil {
						o.Body.Close() //nolint:errcheck // test
					}
					return err
				},
				"Head":   func() error { _, err := s.Head(tc.ctx, "files", "k"); return err },
				"Delete": func() error { return s.Delete(tc.ctx, "files", "k") },
				"List":   func() error { return s.List(tc.ctx, "files", "", func(objects.Entry) error { return nil }) },
				"Ping":   func() error { return s.Ping(tc.ctx) },
			}
			for op, f := range ops {
				if err := f(); !errs.Is(err, tc.kind) {
					t.Errorf("%s with %s ctx: err = %v, want %v", op, name, err, tc.kind)
				}
			}
		}
		// The canceled calls changed nothing.
		if _, err := s.Head(testCtx(t), "files", "k"); err != nil {
			t.Errorf("object lost to a canceled Delete: %v", err)
		}
		mustMissing(t, s, "files", "new")
	})
}

type errReader struct{ err error }

func (r *errReader) Read([]byte) (int, error) { return 0, r.err }

// readAllT reads r to the end, failing the test on a read error.
func readAllT(t testing.TB, r io.Reader) []byte {
	t.Helper()
	b, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	return b
}
