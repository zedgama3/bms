package files_test

import (
	"testing"

	"github.com/zedgama3/bms/internal/core/files"
	"github.com/zedgama3/bms/internal/core/settings"
	"github.com/zedgama3/bms/internal/core/settings/settingstest"
	"github.com/zedgama3/bms/internal/lib/errs"
)

// boundaries step 5: files declares files.max_bytes, files.quota_bytes and
// files.max_per_record itself (files.Settings, formerly package filesettings) and maps
// them onto Policy from Options.Settings on every use case. The mapping is unexported, so
// the tests see max_bytes through Stage's too_large limit; the quota and per-record keys
// are covered by the integration attach tests, which now reach the policy through the same
// settings.Reader.

// Moved from filesettings (declarations): the area validates and its defaults are
// DefaultPolicy, so a deployment that sets nothing gets the spec's 25 MiB / unlimited / 20.
func TestSettings_DeclarationsAreValidAndDefaultToDefaultPolicy(t *testing.T) {
	a := settings.Area{Name: "files", Core: true, Decls: files.Settings}
	if err := a.Validate(); err != nil {
		t.Fatalf("Area.Validate: %v", err)
	}
	f := settingstest.NewFake("files", files.Settings)
	def := files.DefaultPolicy()
	mb, err := f.Int(t.Context(), "max_bytes")
	wantNoErr(t, err)
	qb, err := f.Int(t.Context(), "quota_bytes")
	wantNoErr(t, err)
	pr, err := f.Int(t.Context(), "max_per_record")
	wantNoErr(t, err)
	if mb != def.MaxBytes || qb != def.QuotaBytes || int(pr) != def.MaxPerRecord {
		t.Errorf("declared defaults = %d/%d/%d, want %+v", mb, qb, pr, def)
	}
	if len(files.Settings) != 3 {
		t.Errorf("declares %d keys, want 3", len(files.Settings))
	}
}

// With nothing set, the use case limits an upload at the declared max_bytes.
func TestStage_NothingSet_UsesTheDeclaredMaxBytes(t *testing.T) {
	r := newRig(t, func(_ *rig, o *files.Options) { o.Settings = settingstest.NewFake("files", files.Settings) })
	limit := files.DefaultPolicy().MaxBytes
	_, err := stageBytes(ctxT(t), r.vol, "big.pdf", files.Documents, pdfOfSize(t, int(limit)+1))
	fe := fieldErr(t, err, errs.Invalid, "file", "too_large")
	if fe.Params["max"] != itoa(limit) {
		t.Errorf("max param = %q, want %d", fe.Params["max"], limit)
	}
}

// A reader of another area declares no files key: the use case fails Invalid and never
// falls back to the defaults.
func TestStage_WrongArea_ReturnsInvalid(t *testing.T) {
	r := newRig(t, func(_ *rig, o *files.Options) { o.Settings = settingstest.NewFake("people", nil) })
	_, err := stageBytes(ctxT(t), r.vol, "a.png", files.Images, plainPNG(t))
	wantKind(t, err, errs.Invalid)
	r.wantNoObjects(ownerVol)
}

// An unset key is settings' declared default, not files.DefaultPolicy: the declarations
// here carry another max_bytes default and files must follow it.
func TestPolicy_UnsetKey_IsTheDeclaredDefault(t *testing.T) {
	const declared = 2 << 20
	decls := append([]settings.Decl(nil), files.Settings...)
	for i := range decls {
		if decls[i].Key == "max_bytes" {
			decls[i].Default = itoa(declared)
		}
	}
	r := newRig(t, func(_ *rig, o *files.Options) { o.Settings = settingstest.NewFake("files", decls) })
	_, err := stageBytes(ctxT(t), r.vol, "big.pdf", files.Documents, pdfOfSize(t, declared+1))
	fe := fieldErr(t, err, errs.Invalid, "file", "too_large")
	if fe.Params["max"] != itoa(declared) {
		t.Errorf("max param = %q, want %d (the declared default)", fe.Params["max"], declared)
	}
	if _, err := stageBytes(ctxT(t), r.vol, "ok.pdf", files.Documents, pdfOfSize(t, declared)); err != nil {
		t.Errorf("an upload of exactly the declared default: %v", err)
	}
}

// Read per use case, never cached: a changed setting applies to the next upload.
func TestPolicy_ChangedSetting_AppliesToTheNextUpload(t *testing.T) {
	f := settingstest.NewFake("files", files.Settings)
	r := newRig(t, func(_ *rig, o *files.Options) { o.Settings = f })
	big := pdfOfSize(t, 2<<20)
	if _, err := stageBytes(ctxT(t), r.vol, "a.pdf", files.Documents, big); err != nil {
		t.Fatalf("under the default limit: %v", err)
	}
	f.Set("max_bytes", itoa(1<<20))
	_, err := stageBytes(ctxT(t), r.vol, "a.pdf", files.Documents, big)
	wantField(t, err, errs.Invalid, "file", "too_large")
}
