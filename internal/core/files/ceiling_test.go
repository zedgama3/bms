package files_test

import (
	"strconv"
	"testing"

	"github.com/zedgama3/bms/internal/core/files"
	"github.com/zedgama3/bms/internal/lib/errs"
)

// withCeiling sets Options.Ceiling (boundaries step 10, files.md "Ceiling").
func withCeiling(c files.Policy) rigOption {
	return func(_ *rig, o *files.Options) { o.Ceiling = c }
}

// Boundaries step 10 (F37 moved from demo): each non-zero Ceiling field is a maximum the
// settings-derived policy never exceeds, a smaller setting is kept, and a zero Ceiling is no
// ceiling. MaxBytes is observable through Stage's too_large limit; the quota and per-record
// fields are covered by the integration tests in integration_ceiling_test.go.
func TestNew_CeilingNeverRaises(t *testing.T) {
	const mib = int64(1 << 20)
	tests := []struct {
		name    string
		setting int64
		ceiling int64
		want    int64 // the limit Stage enforces
	}{
		{"a larger setting is lowered to the ceiling", 25 * mib, 2 * mib, 2 * mib},
		{"the largest setting is lowered to the ceiling", files.UploadCeiling, 2 * mib, 2 * mib},
		{"a smaller setting is kept", 1 * mib, 2 * mib, 1 * mib},
		{"exactly at the ceiling", 2 * mib, 2 * mib, 2 * mib},
		{"one above the ceiling is lowered", 2*mib + 1, 2 * mib, 2 * mib},
		{"a zero ceiling is no ceiling", 3 * mib, 0, 3 * mib},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := newRig(t,
				withPolicy(files.Policy{MaxBytes: tc.setting, MaxPerRecord: 20}),
				withCeiling(files.Policy{MaxBytes: tc.ceiling}))
			_, err := stageBytes(ctxT(t), r.vol, "big.pdf", files.Documents, pdfOfSize(t, int(tc.want)+1))
			fe := fieldErr(t, err, errs.Invalid, "file", "too_large")
			if fe.Params["max"] != strconv.FormatInt(tc.want, 10) {
				t.Errorf("max param = %q, want %d", fe.Params["max"], tc.want)
			}
			// Exactly at the limit is accepted.
			if _, err := stageBytes(ctxT(t), r.vol, "ok.pdf", files.Documents, pdfOfSize(t, int(tc.want))); err != nil {
				t.Errorf("an upload of exactly the limit: %v", err)
			}
		})
	}
}

// The ceiling is applied on every use case, so a setting raised later still cannot lift it.
func TestNew_CeilingHoldsWhenTheSettingIsRaisedLater(t *testing.T) {
	const mib = int64(1 << 20)
	r := newRig(t,
		withPolicy(files.Policy{MaxBytes: 1 * mib, MaxPerRecord: 20}),
		withCeiling(files.Policy{MaxBytes: 2 * mib}))
	if _, err := stageBytes(ctxT(t), r.vol, "a.pdf", files.Documents, pdfOfSize(t, int(2*mib))); err == nil {
		t.Fatal("an upload over the setting was accepted")
	}
	r.policy.set(files.Policy{MaxBytes: 100 * mib, MaxPerRecord: 20}) // a settings-admin raises it
	_, err := stageBytes(ctxT(t), r.vol, "a.pdf", files.Documents, pdfOfSize(t, int(2*mib)+1))
	wantField(t, err, errs.Invalid, "file", "too_large")
	if _, err := stageBytes(ctxT(t), r.vol, "a.pdf", files.Documents, pdfOfSize(t, int(2*mib))); err != nil {
		t.Errorf("an upload at the ceiling after the setting was raised: %v", err)
	}
}

// A settings failure is still the use case's failure with the reader's kind: the ceiling is
// not a fallback.
func TestNew_CeilingDoesNotMaskASettingsFailure(t *testing.T) {
	r := newRig(t, withCeiling(files.Policy{MaxBytes: 2 << 20}))
	r.policy.mu.Lock()
	r.policy.err = errs.E("test.policy", errs.Unavailable, errSettingsDown)
	r.policy.mu.Unlock()
	_, err := stageBytes(ctxT(t), r.vol, "a.png", files.Images, plainPNG(t))
	wantKind(t, err, errs.Unavailable)
	r.wantNoObjects(ownerVol)
}
