//go:build integration

package files_test

import (
	"strconv"
	"testing"

	"github.com/zedgama3/bms/internal/core/files"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
)

// A zero QuotaBytes setting is unlimited; with a Ceiling it becomes Ceiling.QuotaBytes. A
// larger setting is lowered to it, a smaller one kept, and a zero ceiling is no ceiling
// (boundaries step 10; F37 moved from demo).
func TestNew_CeilingQuota_NeverRaises(t *testing.T) {
	tests := []struct {
		name    string
		setting int64
		ceiling int64
		max     int64 // the quota the second upload meets; 0 = it fits
	}{
		{"a zero setting becomes the ceiling", 0, 5999, 5999},
		{"a larger setting is lowered", 1 << 30, 5999, 5999},
		{"a smaller setting is kept", 4000, 5999, 4000},
		{"zero setting and zero ceiling is unlimited", 0, 0, 0},
		{"a setting with no ceiling is kept", 4000, 0, 4000},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := newIRig(t,
				withPolicy(files.Policy{MaxBytes: 1 << 20, QuotaBytes: tc.setting, MaxPerRecord: 20}),
				withCeiling(files.Policy{QuotaBytes: tc.ceiling}))
			at := files.Attachment{Record: qualRef(), Level: model.SensitivityInternal}
			if tc.max == 0 || tc.max >= 3000 {
				r.upload(ctxT(t), r.vol, "1.pdf", files.Documents, pdfOfSize(t, 3000), at)
			}
			st := mustStage(t, r.vol, "2.pdf", files.Documents, pdfOfSize(t, 3000))
			_, err := r.attach(ctxT(t), r.vol, st, at)
			if tc.max == 0 {
				wantNoErr(t, err)
				return
			}
			fe := codeErr(t, err, errs.Conflict, "quota")
			if fe.Params["max"] != strconv.FormatInt(tc.max, 10) {
				t.Errorf("max param = %q, want %d", fe.Params["max"], tc.max)
			}
		})
	}
}

func TestNew_CeilingPerRecord_NeverRaises(t *testing.T) {
	tests := []struct {
		name    string
		setting int
		ceiling int
		want    int
	}{
		{"a larger setting is lowered", 20, 2, 2},
		{"a smaller setting is kept", 1, 2, 1},
		{"a zero ceiling is no ceiling", 3, 0, 3},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := newIRig(t,
				withPolicy(files.Policy{MaxBytes: 1 << 20, MaxPerRecord: tc.setting}),
				withCeiling(files.Policy{MaxPerRecord: tc.ceiling}))
			at := files.Attachment{Record: qualRef(), Level: model.SensitivityInternal}
			for i := 0; i < tc.want; i++ {
				r.upload(ctxT(t), r.vol, "p"+strconv.Itoa(i)+".png", files.Images, plainPNG(t), at)
			}
			st := mustStage(t, r.vol, "over.png", files.Images, plainPNG(t))
			_, err := r.attach(ctxT(t), r.vol, st, at)
			fe := codeErr(t, err, errs.Conflict, "too_many")
			if fe.Params["max"] != strconv.Itoa(tc.want) {
				t.Errorf("max param = %q, want %d", fe.Params["max"], tc.want)
			}
		})
	}
}
