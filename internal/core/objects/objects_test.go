package objects_test

import (
	"strings"
	"testing"

	"github.com/zedgama3/bms/internal/core/objects"
	"github.com/zedgama3/bms/internal/lib/errs"
)

// These tests cover the pure checks of the object-store contract (files.md, "The object
// store"): Bucket, Key, Kind and PutInput validation. Store behaviour is in contract_test.go.

func TestBucketValidate(t *testing.T) {
	tests := []struct {
		name string
		in   string
		ok   bool
	}{
		{"single letter", "a", true},
		{"module name", "volunteers", true},
		{"letters and digits", "a1b2c3", true},
		{"max length", "a" + strings.Repeat("9", objects.MaxBucketBytes-1), true},
		{"empty", "", false},
		{"too long", "a" + strings.Repeat("9", objects.MaxBucketBytes), false},
		{"upper case", "Files", false},
		{"leading digit", "1files", false},
		{"dash", "my-files", false},
		{"underscore", "my_files", false},
		{"dot", "a.b", false},
		{"slash", "a/b", false},
		{"space", "a b", false},
		{"non ascii", "café", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := objects.Bucket(tc.in).Validate()
			if tc.ok && err != nil {
				t.Fatalf("Validate(%q) = %v, want nil", tc.in, err)
			}
			if !tc.ok && !errs.Is(err, errs.Invalid) {
				t.Fatalf("Validate(%q) = %v, want Invalid", tc.in, err)
			}
		})
	}
}

func TestKeyValidate(t *testing.T) {
	tests := []struct {
		name string
		in   string
		ok   bool
	}{
		{"uuid", "0190a1b2-c3d4-4e5f-8a6b-0123456789ab", true},
		{"dots dashes underscores", "a.b-c_d", true},
		{"nested", "a/b/c", true},
		{"probe key", string(objects.ProbeKey), true},
		{"max length", strings.Repeat("a", objects.MaxKeyBytes), true},
		{"empty", "", false},
		{"too long", strings.Repeat("a", objects.MaxKeyBytes+1), false},
		{"dot segment", "a/./b", false},
		{"only dot", ".", false},
		{"dotdot segment", "a/../b", false},
		{"only dotdot", "..", false},
		{"empty segment", "a//b", false},
		{"leading slash", "/a", false},
		{"trailing slash", "a/", false},
		{"space", "a b", false},
		{"backslash", `a\b`, false},
		{"nul", "a\x00b", false},
		{"newline", "a\nb", false},
		{"non ascii", "café", false},
		{"question mark", "a?b", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := objects.Key(tc.in).Validate()
			if tc.ok && err != nil {
				t.Fatalf("Validate(%q) = %v, want nil", tc.in, err)
			}
			if !tc.ok && !errs.Is(err, errs.Invalid) {
				t.Fatalf("Validate(%q) = %v, want Invalid", tc.in, err)
			}
		})
	}
}

func TestConstants(t *testing.T) {
	if objects.MaxKeyBytes != 255 || objects.MaxBucketBytes != 32 {
		t.Errorf("MaxKeyBytes/MaxBucketBytes = %d/%d, want 255/32", objects.MaxKeyBytes, objects.MaxBucketBytes)
	}
	if objects.ProbeKey != ".bms-probe" {
		t.Errorf("ProbeKey = %q, want .bms-probe", objects.ProbeKey)
	}
}

func TestParseKind(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want objects.Kind
	}{{"disk", objects.Disk}, {"s3", objects.S3}} {
		got, err := objects.ParseKind(tc.in)
		if err != nil || got != tc.want {
			t.Errorf("ParseKind(%q) = %v, %v; want %v", tc.in, got, err, tc.want)
		}
		if got.String() != tc.in {
			t.Errorf("Kind(%q).String() = %q", tc.in, got.String())
		}
	}
	for _, bad := range []string{"", "postgres", "Disk", "S3", " s3", "disk ", "file"} {
		if _, err := objects.ParseKind(bad); !errs.Is(err, errs.Invalid) {
			t.Errorf("ParseKind(%q) err = %v, want Invalid", bad, err)
		}
	}
}

func TestPutInputValidate(t *testing.T) {
	good := objects.PutInput{Bucket: "files", Key: "k1", Size: 3, ContentType: "image/png"}
	if err := good.Validate(); err != nil {
		t.Fatalf("Validate(good) = %v", err)
	}
	tests := []struct {
		name string
		mut  func(*objects.PutInput)
	}{
		{"bad bucket", func(p *objects.PutInput) { p.Bucket = "Bad-Bucket" }},
		{"empty bucket", func(p *objects.PutInput) { p.Bucket = "" }},
		{"bad key", func(p *objects.PutInput) { p.Key = "a/../b" }},
		{"empty key", func(p *objects.PutInput) { p.Key = "" }},
		{"negative size", func(p *objects.PutInput) { p.Size = -1 }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := good
			tc.mut(&p)
			if err := p.Validate(); !errs.Is(err, errs.Invalid) {
				t.Fatalf("Validate = %v, want Invalid", err)
			}
		})
	}
}
