//nolint:gosec // test code: errors come straight from the fake or store under test; fixtures do bounded arithmetic
package files_test

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/core/audit"
	"github.com/zedgama3/bms/internal/core/files"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
)

func TestConstants(t *testing.T) {
	if files.MaxNameBytes != 255 || files.MaxFields != 32 || files.MaxFieldBytes != 4096 || files.SniffBytes != 512 {
		t.Errorf("limits = %d/%d/%d/%d", files.MaxNameBytes, files.MaxFields, files.MaxFieldBytes, files.SniffBytes)
	}
	if files.UploadCeiling != 1<<30 {
		t.Errorf("UploadCeiling = %d, want 1 GiB", int64(files.UploadCeiling))
	}
	if files.UploadTimeout != 10*time.Minute || files.ServeTimeout != 10*time.Minute {
		t.Errorf("UploadTimeout/ServeTimeout = %v/%v, want 10m", files.UploadTimeout, files.ServeTimeout)
	}
	if files.OrphanAge != 24*time.Hour {
		t.Errorf("OrphanAge = %v, want 24h", files.OrphanAge)
	}
	if files.MaxFilesPerRecordCeiling != 200 {
		t.Errorf("MaxFilesPerRecordCeiling = %d", files.MaxFilesPerRecordCeiling)
	}
	if files.JobReap != "files.reap" || files.ReapEvery != time.Hour {
		t.Errorf("reap job = %q every %v", files.JobReap, files.ReapEvery)
	}
	if files.JobSweepOrphans != "files.sweep-orphans" || files.SweepEvery != 24*time.Hour {
		t.Errorf("sweep job = %q every %v", files.JobSweepOrphans, files.SweepEvery)
	}
	if files.JobTimeout != 30*time.Minute {
		t.Errorf("JobTimeout = %v", files.JobTimeout)
	}
	if files.LogoRecordKind != "settings.logo" {
		t.Errorf("LogoRecordKind = %q", files.LogoRecordKind)
	}
}

func TestDetect(t *testing.T) {
	tests := []struct {
		name string
		head []byte
		want files.Kind
		ok   bool
	}{
		{"png", plainPNG(t), files.PNG, true},
		{"png signature only", []byte("\x89PNG\r\n\x1a\n"), files.PNG, true},
		{"jpeg", plainJPEG(t), files.JPEG, true},
		{"jpeg three bytes", []byte{0xff, 0xd8, 0xff}, files.JPEG, true},
		{"gif87a", []byte("GIF87a\x01\x00\x01\x00"), files.GIF, true},
		{"gif89a", plainGIF(t), files.GIF, true},
		{"webp", plainWebP(), files.WebP, true},
		{"pdf", pdfDoc(""), files.PDF, true},
		{"pdf prefix only", []byte("%PDF-"), files.PDF, true},
		{"empty", nil, 0, false},
		{"one byte", []byte{0xff}, 0, false},
		{"png cut in signature", []byte("\x89PNG\r\n"), 0, false},
		{"html", refusedBodies["html named png"], 0, false},
		{"svg", refusedBodies["svg"], 0, false},
		{"xml", refusedBodies["xml"], 0, false},
		{"zip", refusedBodies["zip"], 0, false},
		{"heic", refusedBodies["heic"], 0, false},
		{"riff wave", refusedBodies["riff but wave"], 0, false},
		{"pdf after junk", refusedBodies["pdf with junk lead"], 0, false},
		{"pdf without dash", refusedBodies["pdf missing dash"], 0, false},
		{"text", refusedBodies["plain text"], 0, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			head := tc.head
			if len(head) > files.SniffBytes {
				head = head[:files.SniffBytes]
			}
			got, ok := files.Detect(head)
			if ok != tc.ok || got != tc.want {
				t.Errorf("Detect = %v, %v; want %v, %v", got, ok, tc.want, tc.ok)
			}
		})
	}
}

func TestKind_Catalogue(t *testing.T) {
	tests := []struct {
		k         files.Kind
		token, ct string
		ext       string
	}{
		{files.PNG, "png", "image/png", ".png"},
		{files.JPEG, "jpeg", "image/jpeg", ".jpg"},
		{files.GIF, "gif", "image/gif", ".gif"},
		{files.WebP, "webp", "image/webp", ".webp"},
		{files.PDF, "pdf", "application/pdf", ".pdf"},
	}
	for _, tc := range tests {
		t.Run(tc.token, func(t *testing.T) {
			if tc.k.String() != tc.token || tc.k.ContentType() != tc.ct || tc.k.Extension() != tc.ext {
				t.Errorf("got %q %q %q", tc.k.String(), tc.k.ContentType(), tc.k.Extension())
			}
			if !tc.k.Inline() {
				t.Error("Inline() = false; every M4 kind is served inline (decision 12)")
			}
			if err := tc.k.Validate(); err != nil {
				t.Errorf("Validate = %v", err)
			}
		})
	}
	for _, bad := range []files.Kind{0, 6, 99} {
		if err := bad.Validate(); !errs.Is(err, errs.Invalid) {
			t.Errorf("Kind(%d).Validate = %v, want Invalid", bad, err)
		}
	}
}

func TestAccept_Presets(t *testing.T) {
	same := func(got files.Accept, want ...files.Kind) bool {
		if len(got) != len(want) {
			return false
		}
		for i := range got {
			if got[i] != want[i] {
				return false
			}
		}
		return true
	}
	if !same(files.Images, files.PNG, files.JPEG, files.GIF, files.WebP) {
		t.Errorf("Images = %v", files.Images)
	}
	if !same(files.Documents, files.PDF) {
		t.Errorf("Documents = %v", files.Documents)
	}
	if !same(files.Brand, files.PNG, files.JPEG, files.WebP) {
		t.Errorf("Brand = %v (no SVG, no GIF, Q-FILES3)", files.Brand)
	}
}

func TestPolicy(t *testing.T) {
	d := files.DefaultPolicy()
	if d.MaxBytes != 25<<20 || d.QuotaBytes != 0 || d.MaxPerRecord != 20 {
		t.Errorf("DefaultPolicy = %+v, want 25 MiB / unlimited / 20", d)
	}
	if err := d.Validate(); err != nil {
		t.Errorf("DefaultPolicy().Validate = %v", err)
	}
	ok := files.Policy{MaxBytes: 1 << 20, QuotaBytes: 5 << 30, MaxPerRecord: files.MaxFilesPerRecordCeiling}
	if err := ok.Validate(); err != nil {
		t.Errorf("Validate(%+v) = %v", ok, err)
	}
	tests := []struct {
		name string
		p    files.Policy
	}{
		{"zero max bytes", files.Policy{MaxBytes: 0, MaxPerRecord: 20}},
		{"negative max bytes", files.Policy{MaxBytes: -1, MaxPerRecord: 20}},
		{"above the route ceiling", files.Policy{MaxBytes: files.UploadCeiling + 1, MaxPerRecord: 20}},
		{"negative quota", files.Policy{MaxBytes: 1 << 20, QuotaBytes: -1, MaxPerRecord: 20}},
		{"zero per record", files.Policy{MaxBytes: 1 << 20, MaxPerRecord: 0}},
		{"above the per-record ceiling", files.Policy{MaxBytes: 1 << 20, MaxPerRecord: files.MaxFilesPerRecordCeiling + 1}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.p.Validate(); !errs.Is(err, errs.Invalid) {
				t.Errorf("Validate = %v, want Invalid", err)
			}
		})
	}
}

func goodFile() files.File {
	return files.File{
		ID: model.NewID[files.FileID](), Record: qualRef(), Subject: newPerson(), UploadedBy: newPerson(),
		Level: model.SensitivityPersonal, Name: "certificate.pdf", ContentType: "application/pdf",
		Size: 1234, SHA256: [32]byte{1}, CreatedAt: time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC),
	}
}

func TestFileValidate(t *testing.T) {
	if err := goodFile().Validate(); err != nil {
		t.Fatalf("Validate(good) = %v", err)
	}
	g := goodFile()
	g.Subject, g.UploadedBy = model.PersonID{}, model.PersonID{} // nobody in particular; an operator
	if err := g.Validate(); err != nil {
		t.Errorf("Validate(no subject, no uploader) = %v", err)
	}
	tests := []struct {
		name string
		mut  func(*files.File)
	}{
		{"zero id", func(f *files.File) { f.ID = files.FileID{} }},
		{"zero record", func(f *files.File) { f.Record = audit.Ref{} }},
		{"zero level", func(f *files.File) { f.Level = 0 }},
		{"level above sensitive", func(f *files.File) { f.Level = model.SensitivitySensitive + 1 }},
		{"empty name", func(f *files.File) { f.Name = "" }},
		{"slash in name", func(f *files.File) { f.Name = "a/b.pdf" }},
		{"backslash in name", func(f *files.File) { f.Name = `a\b.pdf` }},
		{"control char in name", func(f *files.File) { f.Name = "a\x00.pdf" }},
		{"name too long", func(f *files.File) { f.Name = strings.Repeat("n", files.MaxNameBytes+1) }},
		{"zero size", func(f *files.File) { f.Size = 0 }},
		{"negative size", func(f *files.File) { f.Size = -5 }},
		{"empty content type", func(f *files.File) { f.ContentType = "" }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := goodFile()
			tc.mut(&f)
			if err := f.Validate(); !errs.Is(err, errs.Invalid) {
				t.Errorf("Validate = %v, want Invalid", err)
			}
		})
	}
}

// File.LogValue renders the ID only: a name is personal data once the level is.
func TestFileLogValue_IsIDOnly(t *testing.T) {
	f := goodFile()
	f.Name = nameMarker + ".pdf"
	var buf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&buf, nil))
	log.Info("opened", slog.Any("file", f))
	out := buf.String()
	if !strings.Contains(out, f.ID.String()) {
		t.Errorf("log line %q does not carry the file ID", out)
	}
	for _, leak := range []string{nameMarker, "application/pdf", f.Subject.String(), f.UploadedBy.String(), "1234"} {
		if strings.Contains(out, leak) {
			t.Errorf("log line leaks %q: %s", leak, out)
		}
	}
}

func TestStagedValidate(t *testing.T) {
	good := files.Staged{Bucket: ownerVol, Key: "0190a1b2-c3d4-4e5f-8a6b-0123456789ab", Name: "a.pdf", ContentType: "application/pdf", Size: 10, SHA256: [32]byte{1}, StagedAt: time.Now()}
	if err := good.Validate(); err != nil {
		t.Fatalf("Validate(good) = %v", err)
	}
	if err := (files.Staged{}).Validate(); !errs.Is(err, errs.Invalid) {
		t.Errorf("Validate(zero) = %v, want Invalid", err)
	}
	tests := map[string]func(*files.Staged){
		"bad bucket": func(s *files.Staged) { s.Bucket = "Bad-Bucket" },
		"bad key":    func(s *files.Staged) { s.Key = "a/../b" },
		"empty key":  func(s *files.Staged) { s.Key = "" },
		"zero size":  func(s *files.Staged) { s.Size = 0 },
	}
	for name, mut := range tests {
		t.Run(name, func(t *testing.T) {
			s := good
			mut(&s)
			if err := s.Validate(); !errs.Is(err, errs.Invalid) {
				t.Errorf("Validate = %v, want Invalid", err)
			}
		})
	}
}

// F15 (the part Attachment.Validate owns)
func TestAttachmentValidate(t *testing.T) {
	good := files.Attachment{Record: qualRef(), Subject: newPerson(), Level: model.SensitivityPersonal}
	if err := good.Validate(); err != nil {
		t.Fatalf("Validate(good) = %v", err)
	}
	if err := (files.Attachment{Record: qualRef(), Level: model.SensitivityPublic, Replace: true}).Validate(); err != nil {
		t.Errorf("Validate(no subject, replace) = %v", err)
	}
	tests := []struct {
		name        string
		a           files.Attachment
		field, code string
	}{
		{"zero record", files.Attachment{Level: model.SensitivityPersonal}, "record", "required"},
		{"kind without area", files.Attachment{Record: audit.Ref{Kind: "nodot", ID: [16]byte{1}}, Level: model.SensitivityPersonal}, "record", "malformed"},
		{"kind without id", files.Attachment{Record: audit.Ref{Kind: kindQual}, Level: model.SensitivityPersonal}, "record", "malformed"},
		{"id without kind", files.Attachment{Record: audit.Ref{ID: [16]byte{1}}, Level: model.SensitivityPersonal}, "record", "malformed"},
		{"zero level", files.Attachment{Record: qualRef()}, "level", "one_of"},
		{"level above sensitive", files.Attachment{Record: qualRef(), Level: model.SensitivitySensitive + 1}, "level", "one_of"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			wantField(t, tc.a.Validate(), errs.Invalid, tc.field, tc.code)
		})
	}
}
