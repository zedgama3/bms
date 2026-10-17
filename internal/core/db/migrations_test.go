package db_test

import (
	"errors"
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/validate"
)

func mapFS(names ...string) fstest.MapFS {
	m := fstest.MapFS{}
	for _, n := range names {
		m[n] = &fstest.MapFile{Data: []byte("SELECT 1;\n")}
	}
	return m
}

func TestVersion_ParsesContiguousFiles(t *testing.T) {
	v, err := db.Version(mapFS("0001_init.sql", "0002_add_note.sql", "0003_x9_y.sql"))
	if err != nil {
		t.Fatalf("Version: %v", err)
	}
	if v != 3 {
		t.Fatalf("version = %d, want 3", v)
	}
}

func TestVersion_HighestNumberIsVersion(t *testing.T) {
	for n, files := range map[int][]string{
		1: {"0001_init.sql"},
		2: {"0002_b.sql", "0001_a.sql"},
		12: {
			"0001_a.sql", "0002_a.sql", "0003_a.sql", "0004_a.sql", "0005_a.sql", "0006_a.sql",
			"0007_a.sql", "0008_a.sql", "0009_a.sql", "0010_a.sql", "0011_a.sql", "0012_a.sql",
		},
	} {
		v, err := db.Version(mapFS(files...))
		if err != nil || v != n {
			t.Errorf("Version = %d, %v; want %d", v, err, n)
		}
	}
}

// F33
func TestVersion_BadFileName_ReturnsInvalid(t *testing.T) {
	for _, bad := range []string{
		"README", "README.md", "1_init.sql", "01_init.sql", "00001_init.sql", "0002-init.sql",
		"0002_Init.sql", "0002_init.SQL", "0002_init.sql.bak", "0002_.sql", "0002.sql",
		"0002_in it.sql", "0002_init-two.sql", "000a_init.sql",
	} {
		t.Run(bad, func(t *testing.T) {
			m := mapFS("0001_init.sql")
			m[bad] = &fstest.MapFile{Data: []byte("SELECT 1;")}
			_, err := db.Version(m)
			wantKind(t, err, errs.Invalid)
		})
	}
	t.Run("subdirectory", func(t *testing.T) {
		m := mapFS("0001_init.sql", "sub/0002_other.sql")
		_, err := db.Version(m)
		wantKind(t, err, errs.Invalid)
	})
}

// F34
func TestVersion_DuplicateGapStartOrEmpty_ReturnsInvalid(t *testing.T) {
	tests := []struct {
		name  string
		files []string
	}{
		{"duplicate number", []string{"0001_a.sql", "0001_b.sql"}},
		{"duplicate later", []string{"0001_a.sql", "0002_a.sql", "0002_b.sql"}},
		{"gap", []string{"0001_a.sql", "0003_c.sql"}},
		{"does not start at 0001", []string{"0002_b.sql", "0003_c.sql"}},
		{"zero number", []string{"0000_a.sql", "0001_b.sql"}},
		{"no files", nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := db.Version(mapFS(tc.files...))
			wantKind(t, err, errs.Invalid)
		})
	}
}

type unreadableFS struct {
	fstest.MapFS
	bad string
}

var errUnreadable = errors.New("disk on fire")

func (u unreadableFS) Open(name string) (fs.File, error) {
	if name == u.bad {
		return nil, errUnreadable
	}
	f, err := u.MapFS.Open(name)
	if err != nil {
		return nil, err //nolint:wrapcheck // a test double passes errors through
	}
	return f, nil
}

func (u unreadableFS) ReadFile(name string) ([]byte, error) {
	if name == u.bad {
		return nil, errUnreadable
	}
	b, err := u.MapFS.ReadFile(name)
	if err != nil {
		return nil, err //nolint:wrapcheck // a test double passes errors through
	}
	return b, nil
}

// F35
func TestVersion_UnreadableOrEmptyFile_ReturnsInvalid(t *testing.T) {
	t.Run("empty file", func(t *testing.T) {
		m := mapFS("0001_a.sql")
		m["0002_b.sql"] = &fstest.MapFile{Data: nil}
		_, err := db.Version(m)
		wantKind(t, err, errs.Invalid)
	})
	t.Run("unreadable file", func(t *testing.T) {
		u := unreadableFS{MapFS: mapFS("0001_a.sql", "0002_b.sql"), bad: "0002_b.sql"}
		_, err := db.Version(u)
		wantKind(t, err, errs.Invalid)
	})
}

// F32
func TestSetValidate_Table(t *testing.T) {
	files := mapFS("0001_init.sql")
	tests := []struct {
		name   string
		set    db.Set
		ok     bool
		fields int // minimum number of FieldErrors when not ok
	}{
		{"module set", db.Set{Name: "volunteers", Schema: "volunteers", Files: files}, true, 0},
		{"core set", db.Set{Name: "people", Schema: "core", Files: files}, true, 0},
		{"two-letter name", db.Set{Name: "ab", Schema: "ab", Files: files}, true, 0},
		{"32-char name", db.Set{Name: "a" + strings.Repeat("b", 31), Schema: "core", Files: files}, true, 0},
		{"empty name", db.Set{Name: "", Schema: "core", Files: files}, false, 1},
		{"one-char name", db.Set{Name: "a", Schema: "core", Files: files}, false, 1},
		{"33-char name", db.Set{Name: "a" + strings.Repeat("b", 32), Schema: "core", Files: files}, false, 1},
		{"upper-case name", db.Set{Name: "Alpha", Schema: "core", Files: files}, false, 1},
		{"digit first", db.Set{Name: "1abc", Schema: "core", Files: files}, false, 1},
		{"hyphen", db.Set{Name: "a-b", Schema: "core", Files: files}, false, 1},
		{"underscore", db.Set{Name: "a_b", Schema: "core", Files: files}, false, 1},
		{"empty schema", db.Set{Name: "alpha", Schema: "", Files: files}, false, 1},
		{"schema neither core nor name", db.Set{Name: "alpha", Schema: "beta", Files: files}, false, 1},
		{"schema with quote", db.Set{Name: "alpha", Schema: `al"pha`, Files: files}, false, 1},
		{"public schema", db.Set{Name: "alpha", Schema: "public", Files: files}, false, 1},
		{"nil files", db.Set{Name: "alpha", Schema: "alpha"}, false, 1},
		{"all wrong", db.Set{Name: "A", Schema: "x y"}, false, 2},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.set.Validate()
			if tc.ok {
				if err != nil {
					t.Fatalf("want ok, got %v", err)
				}
				return
			}
			wantKind(t, err, errs.Invalid)
			if n := len(validate.Fields(err)); n < tc.fields {
				t.Fatalf("want at least %d FieldErrors, got %d", tc.fields, n)
			}
		})
	}
}
