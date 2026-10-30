//go:build integration

package volunteers_test

import (
	"testing"

	"github.com/zedgama3/bms/internal/modules/volunteers/vol"
)

// The qualification-verified mail carries the record's expiry as vol.Date.Long() text.
func TestVerifyRecord_WithExpiry_MailCarriesLongExpiryDate(t *testing.T) {
	r, q, d, p := uploadScenario(t)
	exp := vol.Date{Year: 2026, Month: 12, Day: 31}
	rec, err := r.svc.UploadCertificate(r.as(p), vol.UploadInput{
		Qualification: q.ID, AcquiredOn: jan1, ExpiresOn: exp, Upload: upload("radio.pdf", pdf()),
	})
	wantNoErr(t, err)
	got, err := r.svc.VerifyRecord(r.as(r.lead(d)), rec.ID, vol.VerifyInput{})
	wantNoErr(t, err)
	sent := r.sentTo("volunteers.qualification-verified")
	if len(sent) != 1 {
		t.Fatalf("sent %d qualification-verified mails, want 1", len(sent))
	}
	if sent[0].Vars["expires_on"] != got.ExpiresOn.Long() || sent[0].Vars["expires_on"] != "31 December 2026" {
		t.Errorf("expires_on = %q, want %q", sent[0].Vars["expires_on"], "31 December 2026")
	}
}

// With no expiry the mail carries no expires_on at all.
func TestVerifyRecord_NoExpiry_MailOmitsExpiryDate(t *testing.T) {
	r, q, d, p := uploadScenario(t)
	rec := pendingRecord(t, r, q, p)
	_, err := r.svc.VerifyRecord(r.as(r.lead(d)), rec.ID, vol.VerifyInput{NoExpiry: true})
	wantNoErr(t, err)
	sent := r.sentTo("volunteers.qualification-verified")
	if len(sent) != 1 {
		t.Fatalf("sent %d mails, want 1", len(sent))
	}
	if v, ok := sent[0].Vars["expires_on"]; ok {
		t.Errorf("expires_on = %q, want absent for a record without expiry", v)
	}
}
