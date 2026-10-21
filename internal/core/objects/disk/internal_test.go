package disk

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/zedgama3/bms/internal/core/objects"
	"github.com/zedgama3/bms/internal/core/objects/objectstest"
)

// TestStore_PassesConformance runs the shared contract suite over a fresh directory.
func TestStore_PassesConformance(t *testing.T) {
	objectstest.Conformance(t, func(t *testing.T) objects.Store {
		s, err := New(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		return s
	})
}

// TestStore_LayoutHoldsNoName pins the on-disk layout: the data file and its .meta sit under
// <bucket>/<aa>/<bb>/ and nothing but the key names them.
func TestStore_LayoutHoldsNoName(t *testing.T) {
	root := t.TempDir()
	s, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	mustPut(t, s, "layout", "0198b5f0-0000-7000-8000-000000000001", []byte("bytes"))
	data := s.dataPath("layout", "0198b5f0-0000-7000-8000-000000000001")
	for _, p := range []string{data, data + ".meta"} {
		st, err := os.Stat(p)
		if err != nil {
			t.Fatalf("stat %s: %v", p, err)
		}
		if st.Mode().Perm() != 0o600 {
			t.Errorf("%s mode = %v, want 0600", filepath.Base(p), st.Mode().Perm())
		}
	}
	rel, relErr := filepath.Rel(root, data)
	if relErr != nil {
		t.Fatalf("Rel: %v", relErr)
	}
	if filepath.Dir(filepath.Dir(filepath.Dir(rel))) != "layout" {
		t.Errorf("data path %q is not <bucket>/<aa>/<bb>/<key>", rel)
	}
}

// TestStore_ReservedSuffixKeys_AreInvalid pins that .meta and .part keys are refused, since
// they would collide with the layout's own files.
func TestStore_ReservedSuffixKeys_AreInvalid(t *testing.T) {
	s, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range []objects.Key{"x.meta", "x.part"} {
		if _, err := s.Head(t.Context(), "layout", k); err == nil {
			t.Errorf("Head(%q) succeeded", k)
		}
	}
}

func mustPut(t *testing.T, s *Store, b objects.Bucket, k objects.Key, data []byte) {
	t.Helper()
	in := objects.PutInput{Bucket: b, Key: k, Size: int64(len(data)), SHA256: sum(data), Body: bytesReader(data)}
	if _, err := s.Put(t.Context(), in); err != nil {
		t.Fatal(err)
	}
}
