//nolint:gosec // test code: errors come straight from the fake or store under test; fixtures do bounded arithmetic
package disk_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/zedgama3/bms/internal/core/objects"
	"github.com/zedgama3/bms/internal/core/objects/disk"
	"github.com/zedgama3/bms/internal/core/objects/objectstest"
	"github.com/zedgama3/bms/internal/lib/errs"
)

func newStore(t *testing.T, dir string) *disk.Store {
	t.Helper()
	s, err := disk.New(dir)
	if err != nil {
		t.Fatalf("disk.New(%q): %v", dir, err)
	}
	return s
}

func putBytes(t *testing.T, s objects.Store, b objects.Bucket, k objects.Key, data []byte) {
	t.Helper()
	_, err := s.Put(t.Context(), objects.PutInput{Bucket: b, Key: k, Size: int64(len(data)), SHA256: sha256.Sum256(data), ContentType: "text/plain", Body: bytes.NewReader(data)})
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
}

func walk(t *testing.T, root string) (files, dirs []string) {
	t.Helper()
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if p == root {
			return nil
		}
		if d.IsDir() {
			dirs = append(dirs, p)
		} else {
			files = append(files, p)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	return files, dirs
}

func TestDisk_Conformance(t *testing.T) {
	objectstest.Conformance(t, func(t *testing.T) objects.Store { return newStore(t, t.TempDir()) })
}

func TestDiskNew_BadDir_ReturnsInvalid(t *testing.T) { // F57
	for _, dir := range []string{"", "relative/dir", ".", "./store", "../store"} {
		if _, err := disk.New(dir); !errs.Is(err, errs.Invalid) {
			t.Errorf("New(%q) err = %v, want Invalid", dir, err)
		}
	}
}

func TestDiskNew_DoesNoIO(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "not", "created", "yet")
	s := newStore(t, dir)
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("New created %s (stat err %v); the root is created by migrate and checked by Ping", dir, err)
	}
	if s.Kind() != objects.Disk {
		t.Errorf("Kind = %v, want Disk", s.Kind())
	}
}

func TestDiskPing_CreatesRootAndLeavesNoProbe(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "store")
	s := newStore(t, dir)
	if err := s.Ping(t.Context()); err != nil {
		t.Fatalf("Ping: %v", err)
	}
	st, err := os.Stat(dir)
	if err != nil || !st.IsDir() {
		t.Fatalf("root not created: %v", err)
	}
	files, _ := walk(t, dir)
	if len(files) != 0 {
		t.Errorf("Ping left files behind: %v", files)
	}
}

func TestDiskPing_RootIsAFile_ReturnsUnavailable(t *testing.T) {
	f := filepath.Join(t.TempDir(), "afile")
	if err := os.WriteFile(f, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	s := newStore(t, f)
	if err := s.Ping(t.Context()); !errs.Is(err, errs.Unavailable) {
		t.Errorf("Ping err = %v, want Unavailable", err)
	}
}

func TestDiskPing_ReadOnlyRoot_ReturnsUnavailable(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("permission bits do not stop root")
	}
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) }) //nolint:errcheck // best effort so TempDir can clean up
	s := newStore(t, dir)
	if err := s.Ping(t.Context()); !errs.Is(err, errs.Unavailable) {
		t.Errorf("Ping err = %v, want Unavailable", err)
	}
	_, err := s.Put(t.Context(), objects.PutInput{Bucket: "files", Key: "k", Size: 1, SHA256: sha256.Sum256([]byte("x")), Body: strings.NewReader("x")})
	if !errs.Is(err, errs.Unavailable) {
		t.Errorf("Put into a read-only root err = %v, want Unavailable", err)
	}
}

// The layout in files.md "Objects and files on disk": <root>/<bucket>/<aa>/<bb>/<key> and
// <key>.meta, where aa/bb are the first two bytes of SHA-256(key) in hex; files 0600 in
// directories 0700; no half-written leftovers.
func TestDisk_Layout_FansOutByKeyHashAndKeepsMeta(t *testing.T) {
	root := t.TempDir()
	s := newStore(t, root)
	key := objects.Key("0190a1b2-c3d4-4e5f-8a6b-0123456789ab")
	putBytes(t, s, "volunteers", key, []byte("hello"))

	h := sha256.Sum256([]byte(key))
	aa, bb := hex.EncodeToString(h[:1]), hex.EncodeToString(h[1:2])
	data := filepath.Join(root, "volunteers", aa, bb, string(key))
	for _, p := range []string{data, data + ".meta"} {
		st, err := os.Stat(p)
		if err != nil {
			t.Fatalf("expected %s: %v", p, err)
		}
		if runtime.GOOS != "windows" && st.Mode().Perm() != 0o600 {
			t.Errorf("%s mode = %v, want 0600", p, st.Mode().Perm())
		}
	}
	got, err := os.ReadFile(data)
	if err != nil || string(got) != "hello" {
		t.Errorf("data file = %q, %v; plain bytes expected (rsync/restic/tar must work on them)", got, err)
	}
	files, dirs := walk(t, filepath.Join(root, "volunteers"))
	if len(files) != 2 {
		t.Errorf("files = %v, want exactly the data file and its .meta", files)
	}
	if runtime.GOOS != "windows" {
		for _, d := range dirs {
			st, err := os.Stat(d)
			if err != nil {
				t.Fatal(err)
			}
			if st.Mode().Perm() != 0o700 {
				t.Errorf("dir %s mode = %v, want 0700", d, st.Mode().Perm())
			}
		}
	}
}

func TestDisk_MetaIsSmallJSONWithoutNames(t *testing.T) {
	root := t.TempDir()
	s := newStore(t, root)
	putBytes(t, s, "files", "k1", []byte("12345"))
	files, _ := walk(t, root)
	var meta string
	for _, f := range files {
		if strings.HasSuffix(f, ".meta") {
			b, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			meta = string(b)
		}
	}
	if meta == "" {
		t.Fatalf("no .meta among %v", files)
	}
	for _, field := range []string{`"size"`, `"sha256"`, `"content_type"`, `"modified"`} {
		if !strings.Contains(meta, field) {
			t.Errorf(".meta lacks %s: %s", field, meta)
		}
	}
}

func TestDisk_FailedPut_LeavesNoFilesBehind(t *testing.T) {
	root := t.TempDir()
	s := newStore(t, root)
	data := []byte("abcdef")
	bad := sha256.Sum256([]byte("different"))
	if _, err := s.Put(t.Context(), objects.PutInput{Bucket: "files", Key: "k", Size: int64(len(data)), SHA256: bad, Body: bytes.NewReader(data)}); !errs.Is(err, errs.Invalid) {
		t.Fatalf("Put err = %v, want Invalid", err)
	}
	if _, err := s.Put(t.Context(), objects.PutInput{Bucket: "files", Key: "k2", Size: 100, SHA256: sha256.Sum256(data), Body: bytes.NewReader(data)}); !errs.Is(err, errs.Invalid) {
		t.Fatalf("short Put err = %v, want Invalid", err)
	}
	files, _ := walk(t, root)
	if len(files) != 0 {
		t.Errorf("failed Puts left files (.part or otherwise): %v", files)
	}
}

// "a data file's existence means the object is complete": a leftover .part from a crash is not
// an object, is not listed, and does not block a later Put of the same key.
func TestDisk_LeftoverPartFile_IsNotAnObject(t *testing.T) {
	root := t.TempDir()
	s := newStore(t, root)
	putBytes(t, s, "files", "seed", []byte("s"))
	files, _ := walk(t, root)
	if len(files) == 0 {
		t.Fatal("no files after Put")
	}
	dir := filepath.Dir(files[0])
	for _, name := range []string{"half.part", "half.meta.part"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("partial"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	var keys []string
	err := s.List(t.Context(), "files", "", func(e objects.Entry) error { keys = append(keys, string(e.Key)); return nil })
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 1 || keys[0] != "seed" {
		t.Errorf("List = %v, want [seed] (no .meta, no .part entries)", keys)
	}
}

func TestDisk_NamesNeverReachThePath(t *testing.T) {
	root := t.TempDir()
	s := newStore(t, root)
	putBytes(t, s, "volunteers", "0190a1b2-c3d4-4e5f-8a6b-0123456789ab", []byte("%PDF-1.4 Zorbl Quillfeather"))
	files, dirs := walk(t, root)
	for _, p := range append(files, dirs...) {
		if strings.Contains(strings.ToLower(p), "quillfeather") {
			t.Errorf("path %s carries personal data", p)
		}
	}
}

func TestDisk_PersistsAcrossStores(t *testing.T) {
	root := t.TempDir()
	putBytes(t, newStore(t, root), "files", "k", []byte("durable"))
	obj, err := newStore(t, root).Get(t.Context(), "files", "k")
	if err != nil {
		t.Fatalf("Get through a new Store: %v", err)
	}
	defer obj.Body.Close() //nolint:errcheck // test
	buf := make([]byte, 16)
	n, rerr := io.ReadFull(obj.Body, buf[:len("durable")])
	if rerr != nil {
		t.Fatalf("read: %v", rerr)
	}
	if string(buf[:n]) != "durable" {
		t.Errorf("read %q", buf[:n])
	}
}
