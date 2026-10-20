//go:build integration

package notify_test

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/core/audit"
	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/notify"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
)

func wantConflictField(t testing.TB, err error, field, code string) {
	t.Helper()
	wantKind(t, err, errs.Conflict)
	if !hasField(err, field, code) {
		t.Fatalf("no field error %s/%s in %v", field, code, describe(err))
	}
}

func refOf(kind, tail string) audit.Ref {
	sum := sha256.Sum256([]byte(kind + ":" + tail))
	var id [16]byte
	copy(id[:], sum[:16])
	return audit.Ref{Kind: kind, ID: id}
}

// ---- templates ---------------------------------------------------------------------------

func TestPutTemplate_Saves_Audits_AndShowsInGet(t *testing.T) {
	r := newIRig(t)
	ctx := r.admin(string(permTemplates))
	text := notify.TemplateText{Subject: "New {{.Site.Name}}", Body: "Line one\nLine\ttwo {{.Link}}"}
	view, err := r.svc.PutTemplate(ctx, "volunteers.greet", "en", text)
	wantNoErr(t, err)
	if !view.Overridden || view.Current != text || view.Name != "volunteers.greet" || view.Language != "en" {
		t.Fatalf("view = %+v", view)
	}
	if view.Default.Subject == "" || view.Default == text {
		t.Fatalf("the shipped default must stay readable: %+v", view.Default)
	}
	got, err := r.svc.GetTemplate(ctx, "volunteers.greet", "en")
	wantNoErr(t, err)
	if !got.Overridden || got.Current != text {
		t.Fatalf("Get = %+v", got)
	}
	if len(got.Vars) != 2 {
		t.Errorf("Vars = %v, want the Vars: header (shift, note)", got.Vars)
	}
	es, err := r.svc.GetTemplate(ctx, "volunteers.greet", "es")
	wantNoErr(t, err)
	if es.Overridden {
		t.Error("overriding en overrode es")
	}
	rec := r.rec.Recorded()
	if len(rec) != 1 || rec[0].Action != "notify.template.changed" {
		t.Fatalf("audit = %v", actionsOf(rec))
	}
	if rec[0].Record != refOf("notify.template", "volunteers.greet:en") {
		t.Errorf("audit record = %+v", rec[0].Record)
	}
	if rec[0].Reason != "template:volunteers.greet:en" {
		t.Errorf("audit reason = %q", rec[0].Reason)
	}
	list, err := r.svc.ListTemplates(ctx)
	wantNoErr(t, err)
	for _, ti := range list {
		if ti.Name != "volunteers.greet" {
			continue
		}
		over := map[notify.Language]bool{}
		for _, l := range ti.Languages {
			over[l.Language] = l.Overridden
		}
		if !over["en"] || over["es"] {
			t.Errorf("languages = %+v", ti.Languages)
		}
	}
	// Saving again replaces.
	text2 := notify.TemplateText{Subject: "Again", Body: "Again"}
	_, err = r.svc.PutTemplate(ctx, "volunteers.greet", "en", text2)
	wantNoErr(t, err)
	got = first(r.svc.GetTemplate(ctx, "volunteers.greet", "en"))
	if got.Current != text2 {
		t.Fatalf("second Put not stored: %+v", got.Current)
	}
}

func TestPutTemplate_Validation_NothingSaved(t *testing.T) {
	for _, tc := range templateValidationCases() {
		t.Run(tc.name, func(t *testing.T) {
			r := newIRig(t)
			ctx := r.admin(string(permTemplates))
			_, err := r.svc.PutTemplate(ctx, "volunteers.greet", "en", tc.text)
			wantField(t, err, tc.field, tc.code)
			got, gerr := r.svc.GetTemplate(ctx, "volunteers.greet", "en")
			wantNoErr(t, gerr)
			if got.Overridden {
				t.Fatal("an invalid override was stored")
			}
			if len(r.rec.Recorded()) != 0 {
				t.Fatal("audited a rejected save")
			}
		})
	}
	t.Run("newline and tab in a body are fine", func(t *testing.T) {
		r := newIRig(t)
		_, err := r.svc.PutTemplate(r.admin(string(permTemplates)), "volunteers.greet", "en", notify.TemplateText{Subject: "s", Body: "a\n\tb"})
		wantNoErr(t, err)
	})
	t.Run("body exactly at the cap", func(t *testing.T) {
		r := newIRig(t)
		_, err := r.svc.PutTemplate(r.admin(string(permTemplates)), "volunteers.greet", "en",
			notify.TemplateText{Subject: "s", Body: strings.Repeat("b", notify.MaxTemplateBytes)})
		wantNoErr(t, err)
	})
}

func TestResetTemplate_RemovesOverride_Audits(t *testing.T) {
	r := newIRig(t)
	ctx := r.admin(string(permTemplates))
	_, err := r.svc.PutTemplate(ctx, "volunteers.greet", "en", notify.TemplateText{Subject: "s", Body: "b"})
	wantNoErr(t, err)
	wantNoErr(t, r.svc.ResetTemplate(ctx, "volunteers.greet", "en"))
	got, err := r.svc.GetTemplate(ctx, "volunteers.greet", "en")
	wantNoErr(t, err)
	if got.Overridden || got.Current != got.Default {
		t.Fatalf("after reset: %+v", got)
	}
	acts := actionsOf(r.rec.Recorded())
	if len(acts) != 2 || acts[1] != "notify.template.reset" {
		t.Fatalf("audit = %v", acts)
	}
	// F44: no override left; still nil.
	wantNoErr(t, r.svc.ResetTemplate(ctx, "volunteers.greet", "en"))
	wantNoErr(t, r.svc.ResetTemplate(ctx, "volunteers.greet", "es"))
}

func TestPutTemplate_ReadOnly_Unavailable(t *testing.T) {
	r := newIRig(t)
	ctx := r.admin(string(permTemplates))
	r.flt.set(true, nil)
	_, err := r.svc.PutTemplate(ctx, "volunteers.greet", "en", notify.TemplateText{Subject: "s", Body: "b"})
	wantKind(t, err, errs.Unavailable)
	if !errors.Is(err, db.ErrReadOnly) {
		t.Fatalf("does not wrap db.ErrReadOnly: %v", err)
	}
	err = r.svc.ResetTemplate(ctx, "volunteers.greet", "en")
	wantKind(t, err, errs.Unavailable)
	if !errors.Is(err, db.ErrReadOnly) {
		t.Fatalf("Reset does not wrap db.ErrReadOnly: %v", err)
	}
	if _, err := r.svc.GetTemplate(ctx, "volunteers.greet", "en"); err != nil {
		t.Fatalf("reads must work on a read-only copy: %v", err)
	}
	if _, err := r.svc.Outbox(r.admin(string(permOutbox))); err != nil {
		t.Fatalf("Outbox must work on a read-only copy: %v", err)
	}
}

func TestPutTemplate_AuditFails_NothingSaved(t *testing.T) {
	r := newIRig(t)
	ctx := r.admin(string(permTemplates))
	r.rec.Fail(errs.E("test.audit", errs.Unavailable, errors.New("audit down")))
	_, err := r.svc.PutTemplate(ctx, "volunteers.greet", "en", notify.TemplateText{Subject: "s", Body: "b"})
	wantKind(t, err, errs.Unavailable)
	r.rec.Fail(nil)
	if got := first(r.svc.GetTemplate(ctx, "volunteers.greet", "en")); got.Overridden {
		t.Fatal("the override was saved although the audit record failed")
	}
	// Reset with an existing override survives a failing audit.
	_, err = r.svc.PutTemplate(ctx, "volunteers.greet", "en", notify.TemplateText{Subject: "s", Body: "b"})
	wantNoErr(t, err)
	r.rec.Fail(errs.E("test.audit", errs.Unavailable, errors.New("audit down")))
	wantKind(t, r.svc.ResetTemplate(ctx, "volunteers.greet", "en"), errs.Unavailable)
	r.rec.Fail(nil)
	if got := first(r.svc.GetTemplate(ctx, "volunteers.greet", "en")); !got.Overridden {
		t.Fatal("the override was removed although the audit record failed")
	}
}

func TestPreview_RendersWithPlaceholders_SavesNothing(t *testing.T) {
	r := newIRig(t)
	ctx := r.admin(string(permTemplates))
	out, err := r.svc.Preview(ctx, "volunteers.greet", "en", notify.TemplateText{
		Subject: "Hi {{.Site.Name}}", Body: "{{.Vars.shift}} / {{.Vars.note}} / {{term \"shift\"}} / {{.Link}}"})
	wantNoErr(t, err)
	if out.Subject != "Hi Test Burn" {
		t.Errorf("Subject = %q", out.Subject)
	}
	for _, want := range []string{"shift / note", "duty", siteURL} {
		if !strings.Contains(out.Body, want) {
			t.Errorf("Body %q lacks %q (each declared var shows its own name)", out.Body, want)
		}
	}
	if got := first(r.svc.GetTemplate(ctx, "volunteers.greet", "en")); got.Overridden {
		t.Fatal("Preview saved something")
	}
	if len(r.rec.Recorded()) != 0 || r.count() != 0 {
		t.Fatal("Preview audited or queued something")
	}
}

// ---- SendTest ----------------------------------------------------------------------------

func TestSendTest_QueuesToTheActor_Audited(t *testing.T) {
	r := newIRig(t)
	me := r.dbPerson("me@example.org")
	r.az.Grant(me, permTemplates)
	id, err := r.svc.SendTest(as(ctxT(t), me))
	wantNoErr(t, err)
	if id == (notify.MessageID{}) {
		t.Fatal("zero message ID")
	}
	x := r.one(me)
	if x.ID != [16]byte(id) || x.Template != "notify.test" || x.Status != "queued" {
		t.Fatalf("row = %+v", x)
	}
	if len(r.ch.envelopes()) != 0 {
		t.Fatal("SendTest sent inline instead of through the queue")
	}
	rec := r.rec.Recorded()
	if len(rec) != 1 || rec[0].Action != "notify.test.sent" {
		t.Fatalf("audit = %v", actionsOf(rec))
	}
	r.sendOnce()
	if envs := r.ch.envelopes(); len(envs) != 1 || envs[0].To != "me@example.org" {
		t.Fatalf("envelopes = %+v", envs)
	}
}

func TestResetTemplate_NoOverride_Nil(t *testing.T) {
	r := newIRig(t)
	ctx := r.admin(string(permTemplates))
	wantNoErr(t, r.svc.ResetTemplate(ctx, "volunteers.greet", "en"))
	wantNoErr(t, r.svc.ResetTemplate(ctx, "volunteers.greet", "es"))
	if got := first(r.svc.GetTemplate(ctx, "volunteers.greet", "en")); got.Overridden {
		t.Fatal("a reset created an override")
	}
}

func TestSendTest_NoAddress_RowFails(t *testing.T) {
	r := newIRig(t)
	me := r.dbPerson("")
	r.az.Grant(me, permTemplates)
	_, err := r.svc.SendTest(as(ctxT(t), me))
	wantNoErr(t, err)
	r.sendOnce()
	if x := r.one(me); x.Status != "failed" || *x.LastErr != "no_address" {
		t.Fatalf("row = %+v", x)
	}
}

func TestSendTest_NotConfigured_Unsupported(t *testing.T) {
	r := newIRig(t)
	me := r.dbPerson("me@example.org")
	r.az.Grant(me, permTemplates)
	r.smtp.set(func(c *notify.SMTPConfig) { c.Host = "" })
	_, err := r.svc.SendTest(as(ctxT(t), me))
	wantKind(t, err, errs.Unsupported)
	if !errors.Is(err, notify.ErrNotConfigured) {
		t.Fatal("does not wrap ErrNotConfigured")
	}
	if r.count() != 0 || len(r.rec.Recorded()) != 0 {
		t.Fatal("a failed test left a row or an audit entry")
	}
}

func TestSendTest_AuditFails_NothingQueued(t *testing.T) {
	r := newIRig(t)
	me := r.dbPerson("me@example.org")
	r.az.Grant(me, permTemplates)
	r.rec.Fail(errs.E("test.audit", errs.Unavailable, errors.New("down")))
	_, err := r.svc.SendTest(as(ctxT(t), me))
	wantKind(t, err, errs.Unavailable)
	if r.count() != 0 {
		t.Fatal("a row was queued although the audit record failed")
	}
}

// ---- outbox ------------------------------------------------------------------------------

func TestOutbox_CountsConfiguredAndRecent(t *testing.T) {
	r := newIRig(t)
	ctx := r.admin(string(permOutbox))
	queued := r.queue("q@example.org")
	sentP := r.queue("s@example.org")
	failP := r.queue("f@example.org")
	r.exec("UPDATE core.notify_message SET status='sent', sent_at=now(), subject=NULL, body=NULL WHERE person_id=$1", [16]byte(sentP))
	r.exec("UPDATE core.notify_message SET status='failed', failed_at=now(), last_error='rejected' WHERE person_id=$1", [16]byte(failP))

	v, err := r.svc.Outbox(ctx)
	wantNoErr(t, err)
	if !v.Configured {
		t.Error("Configured = false")
	}
	if v.Counts[statusOf(t, "queued")] != 1 || v.Counts[statusOf(t, "sent")] != 1 || v.Counts[statusOf(t, "failed")] != 1 {
		t.Errorf("Counts = %v", v.Counts)
	}
	if len(v.Recent) != 2 {
		t.Fatalf("Recent has %d rows, want the queued and the failed one (not the sent)", len(v.Recent))
	}
	for _, rec := range v.Recent {
		if rec.Status == statusOf(t, "sent") {
			t.Error("a sent row is listed as recent")
		}
		if rec.Subject != "" {
			t.Errorf("Outbox exposes a subject: %q", rec.Subject)
		}
		if rec.Person != queued && rec.Person != failP {
			t.Errorf("unexpected row %+v", rec)
		}
	}
	raw, err := json.Marshal(v)
	wantNoErr(t, err)
	for _, leak := range []string{"q@example.org", "f@example.org", "s@example.org", "Gate 06:00", "address", "\"body\""} {
		if strings.Contains(string(raw), leak) {
			t.Errorf("Outbox JSON carries %q: %s", leak, raw)
		}
	}
	r.smtp.set(func(c *notify.SMTPConfig) { c.Host = "" })
	v, err = r.svc.Outbox(ctx)
	wantNoErr(t, err)
	if v.Configured {
		t.Error("Configured = true with no host")
	}
}

func TestOutbox_RecentCapped_NewestFirst(t *testing.T) {
	r := newIRig(t)
	for i := 0; i < notify.MaxOutboxList+10; i++ {
		r.queue("a@example.org")
		r.advance(time.Second)
	}
	v, err := r.svc.Outbox(r.admin(string(permOutbox)))
	wantNoErr(t, err)
	if len(v.Recent) != notify.MaxOutboxList {
		t.Fatalf("Recent has %d rows, want %d", len(v.Recent), notify.MaxOutboxList)
	}
	for i := 1; i < len(v.Recent); i++ {
		if v.Recent[i].CreatedAt.After(v.Recent[i-1].CreatedAt) {
			t.Fatalf("Recent not newest first at %d", i)
		}
	}
	if total := v.Counts[statusOf(t, "queued")]; total != notify.MaxOutboxList+10 {
		t.Errorf("Counts[queued] = %d", total)
	}
}

func TestOutbox_Empty(t *testing.T) {
	r := newIRig(t)
	v, err := r.svc.Outbox(r.admin(string(permOutbox)))
	wantNoErr(t, err)
	if len(v.Recent) != 0 {
		t.Fatalf("Recent = %v", v.Recent)
	}
}

func rejectRow(t *testing.T, r *irig, addr string) (model.PersonID, row) {
	t.Helper()
	p := r.queue(addr)
	r.ch.failWith(errs.E("test.ch", errs.Invalid, notify.ErrRejected))
	r.sendOnce()
	r.ch.script(nil)
	x := r.one(p)
	if x.Status != "failed" {
		t.Fatalf("setup: %+v", x)
	}
	return p, x
}

func TestRetry_Failed_SendsOriginalText(t *testing.T) {
	r := newIRig(t)
	ctx := r.admin(string(permOutbox))
	p, failed := rejectRow(t, r, "a@example.org")
	if failed.Subject == nil || failed.Body == nil {
		t.Fatal("a failed row lost its text")
	}
	wantNoErr(t, r.svc.Retry(ctx, notify.MessageID(failed.ID)))
	x := r.one(p)
	if x.Status != "queued" || x.Attempts != 0 || x.Next.After(r.clk.Now()) {
		t.Fatalf("after Retry: %+v", x)
	}
	if !x.Expires.Equal(failed.Expires) {
		t.Errorf("Retry moved expires_at: %v -> %v", failed.Expires, x.Expires)
	}
	rec := r.rec.Recorded()
	if len(rec) != 1 || rec[0].Action != "notify.message.retried" || rec[0].Record.Kind != "notify.message" || rec[0].Record.ID != failed.ID {
		t.Fatalf("audit = %+v", rec)
	}
	if got := r.sendOnce(); got.Sent != 1 {
		t.Fatalf("Sent = %+v", got)
	}
	envs := r.ch.envelopes()
	if len(envs) != 1 || envs[0].Subject != *failed.Subject || envs[0].Body != *failed.Body {
		t.Fatalf("the retried message is not the original: %+v", envs)
	}
}

func TestRetry_Expired_Conflict(t *testing.T) {
	for _, expireFirst := range []bool{false, true} {
		name := "text still present"
		if expireFirst {
			name = "after ExpireStale"
		}
		t.Run(name, func(t *testing.T) {
			r := newIRig(t)
			ctx := r.admin(string(permOutbox))
			p := r.dbPerson("a@example.org")
			m := greet(p)
			m.TTL = time.Hour
			wantNoErr(t, r.vol.Send(ctxT(t), m))
			r.ch.failWith(errs.E("test.ch", errs.Invalid, notify.ErrRejected))
			r.sendOnce()
			failed := r.one(p)
			r.advance(2 * time.Hour)
			if expireFirst {
				_, err := r.svc.ExpireStale(ctxT(t))
				wantNoErr(t, err)
			}
			err := r.svc.Retry(ctx, notify.MessageID(failed.ID))
			wantConflictField(t, err, "status", "expired")
			if x := r.one(p); x.Status != "failed" {
				t.Fatalf("row = %+v", x)
			}
			if len(r.rec.Recorded()) != 0 {
				t.Fatal("audited a refused retry")
			}
		})
	}
}

func TestRetry_Unknown_NotFound(t *testing.T) {
	r := newIRig(t)
	wantKind(t, r.svc.Retry(r.admin(string(permOutbox)), model.NewID[notify.MessageID]()), errs.NotFound)
}

func TestRetry_NotFailed_Conflict(t *testing.T) {
	for _, status := range []string{"queued", "sending", "sent"} {
		t.Run(status, func(t *testing.T) {
			r := newIRig(t)
			p := r.queue("a@example.org")
			x := r.one(p)
			if status != "queued" {
				r.force(x.ID, status)
			}
			err := r.svc.Retry(r.admin(string(permOutbox)), notify.MessageID(x.ID))
			wantConflictField(t, err, "status", "")
			if got := r.byID(x.ID); got.Status != status {
				t.Fatalf("status changed to %s", got.Status)
			}
		})
	}
}

func TestCancel_QueuedAndFailed_Deleted_Audited(t *testing.T) {
	r := newIRig(t)
	ctx := r.admin(string(permOutbox))
	q := r.queue("q@example.org")
	_, failed := rejectRow(t, r, "f@example.org")
	qid := r.one(q).ID
	wantNoErr(t, r.svc.Cancel(ctx, notify.MessageID(qid)))
	wantNoErr(t, r.svc.Cancel(ctx, notify.MessageID(failed.ID)))
	if r.count() != 0 {
		t.Fatalf("%d rows left", r.count())
	}
	acts := actionsOf(r.rec.Recorded())
	if len(acts) != 2 || acts[0] != "notify.message.canceled" {
		t.Fatalf("audit = %v", acts)
	}
	if got := r.sendOnce(); got != (notify.Sent{}) {
		t.Fatalf("a canceled message was sent: %+v", got)
	}
	wantKind(t, r.svc.Cancel(ctx, notify.MessageID(qid)), errs.NotFound)
}

func TestCancel_WrongStatus_Conflict(t *testing.T) {
	for _, status := range []string{"sending", "sent"} {
		t.Run(status, func(t *testing.T) {
			r := newIRig(t)
			p := r.queue("a@example.org")
			x := r.one(p)
			r.force(x.ID, status)
			wantConflictField(t, r.svc.Cancel(r.admin(string(permOutbox)), notify.MessageID(x.ID)), "status", "")
			if r.count() != 1 {
				t.Fatal("the row was deleted")
			}
		})
	}
}

func TestOutboxAdmin_AuditFails_RowUntouched(t *testing.T) {
	r := newIRig(t)
	ctx := r.admin(string(permOutbox))
	_, failed := rejectRow(t, r, "a@example.org")
	r.rec.Fail(errs.E("test.audit", errs.Unavailable, errors.New("down")))
	wantKind(t, r.svc.Retry(ctx, notify.MessageID(failed.ID)), errs.Unavailable)
	wantKind(t, r.svc.Cancel(ctx, notify.MessageID(failed.ID)), errs.Unavailable)
	if x := r.byID(failed.ID); x.Status != "failed" {
		t.Fatalf("row = %+v", x)
	}
}

func TestRetryCancel_ReadOnly_Unavailable(t *testing.T) {
	r := newIRig(t)
	ctx := r.admin(string(permOutbox))
	_, failed := rejectRow(t, r, "a@example.org")
	r.flt.set(true, nil)
	for _, err := range []error{r.svc.Retry(ctx, notify.MessageID(failed.ID)), r.svc.Cancel(ctx, notify.MessageID(failed.ID))} {
		wantKind(t, err, errs.Unavailable)
		if !errors.Is(err, db.ErrReadOnly) {
			t.Fatalf("does not wrap db.ErrReadOnly: %v", err)
		}
	}
}

func TestAdmin_RealPermissions_AllowedRoleSucceeds(t *testing.T) {
	r := newIRig(t)
	for _, ac := range adminCalls() {
		perm := ac.perm
		err := ac.call(r.rig, r.admin(string(perm)))
		if errs.Is(err, errs.Permission) || errs.Is(err, errs.Unauthenticated) {
			t.Errorf("%s refused its own permission: %v", ac.name, err)
		}
	}
	// Retry/Cancel of an unknown ID is NotFound, not Permission.
	wantKind(t, r.svc.Retry(r.admin(string(permOutbox)), model.NewID[notify.MessageID]()), errs.NotFound)
	wantKind(t, r.svc.Cancel(r.admin(string(permOutbox)), model.NewID[notify.MessageID]()), errs.NotFound)
}

// ---- export and deletion -----------------------------------------------------------------

func TestExportPerson_NoBodyNoAddress_OnlyThatPerson(t *testing.T) {
	r := newIRig(t)
	p := r.queue("exported@example.org")
	other := r.queue("other@example.org")
	ignore(other)
	raw, err := r.svc.ExportPerson(ctxT(t), p)
	wantNoErr(t, err)
	var doc struct {
		Messages []map[string]any `json:"messages"`
	}
	wantNoErr(t, json.Unmarshal(raw, &doc))
	if len(doc.Messages) != 1 {
		t.Fatalf("export holds %d messages: %s", len(doc.Messages), raw)
	}
	m := doc.Messages[0]
	for _, key := range []string{"id", "template", "status", "language", "created_at"} {
		if _, ok := m[key]; !ok {
			t.Errorf("export lacks %q: %v", key, m)
		}
	}
	for _, banned := range []string{"body", "address"} {
		if _, ok := m[banned]; ok {
			t.Errorf("export carries %q", banned)
		}
	}
	for _, leak := range []string{"exported@example.org", "other@example.org", "bring water"} {
		if strings.Contains(string(raw), leak) {
			t.Errorf("export carries %q", leak)
		}
	}
	if m["subject"] == nil {
		t.Error("subject must be exported while still present")
	}
	r.sendOnce()
	raw, err = r.svc.ExportPerson(ctxT(t), p)
	wantNoErr(t, err)
	var after struct {
		Messages []map[string]any `json:"messages"`
	}
	wantNoErr(t, json.Unmarshal(raw, &after))
	if len(after.Messages) != 1 {
		t.Fatalf("export = %s", raw)
	}
	if s, ok := after.Messages[0]["subject"]; ok && s != nil && s != "" {
		t.Errorf("subject exported after delivery cleared it: %v", s)
	}
	if after.Messages[0]["status"] != "sent" || after.Messages[0]["sent_at"] == nil {
		t.Errorf("export = %v", after.Messages[0])
	}
}

func TestExportPerson_NoRows_EmptyList(t *testing.T) {
	r := newIRig(t)
	raw, err := r.svc.ExportPerson(ctxT(t), r.dbPerson(""))
	wantNoErr(t, err)
	var doc struct {
		Messages []any `json:"messages"`
	}
	wantNoErr(t, json.Unmarshal(raw, &doc))
	if len(doc.Messages) != 0 {
		t.Fatalf("export = %s", raw)
	}
}

func TestDeletePerson_DeletesRows_QueuedNeverSent(t *testing.T) {
	r := newIRig(t)
	p := r.queue("a@example.org")
	r.queue("keep@example.org")
	var n int
	err := r.pool.Transact(ctxT(t), db.Write, func(ctx context.Context) error {
		var derr error
		n, derr = r.svc.DeletePerson(ctx, p)
		if derr != nil {
			return fmt.Errorf("delete person: %w", derr)
		}
		return nil
	})
	wantNoErr(t, err)
	if n != 1 || len(r.rowsFor(p)) != 0 {
		t.Fatalf("deleted %d, left %d", n, len(r.rowsFor(p)))
	}
	if r.count() != 1 {
		t.Fatal("another person's row was deleted")
	}
	if got := r.sendOnce(); got.Sent != 1 {
		t.Fatalf("Sent = %+v, only the other person's message remains", got)
	}
	// The foreign key no longer blocks removing the person.
	r.exec("DELETE FROM core.person WHERE id = $1", [16]byte(p))
}

func TestDeletePerson_NoRows_Zero_AndRollbackKeepsRows(t *testing.T) {
	r := newIRig(t)
	n, err := r.svc.DeletePerson(ctxT(t), r.dbPerson(""))
	wantNoErr(t, err)
	if n != 0 {
		t.Fatalf("n = %d", n)
	}
	p := r.queue("a@example.org")
	boom := errors.New("boom")
	err = r.pool.Transact(ctxT(t), db.Write, func(ctx context.Context) error {
		if _, derr := r.svc.DeletePerson(ctx, p); derr != nil {
			return fmt.Errorf("delete person: %w", derr)
		}
		return boom
	})
	if !errors.Is(err, boom) || len(r.rowsFor(p)) != 1 {
		t.Fatalf("rollback: err=%v rows=%d", err, len(r.rowsFor(p)))
	}
}

func TestDeletePerson_FKRestrictsWhileRowsExist(t *testing.T) {
	r := newIRig(t)
	p := r.queue("a@example.org")
	if _, err := r.pool.Exec(ctxT(t), "DELETE FROM core.person WHERE id = $1", [16]byte(p)); err == nil {
		t.Fatal("deleting a person with outbox rows must be refused (ON DELETE RESTRICT)")
	}
}

func TestDeletePerson_StoreFails_Unavailable(t *testing.T) {
	r := newIRig(t)
	p := r.queue("a@example.org")
	r.flt.set(false, errs.E("test.store", errs.Unavailable, errors.New("down")))
	_, err := r.svc.DeletePerson(ctxT(t), p)
	wantKind(t, err, errs.Unavailable)
	r.flt.set(false, nil)
	if len(r.rowsFor(p)) != 1 {
		t.Fatal("rows lost")
	}
}

func TestDeletePerson_ReadOnly_Unavailable(t *testing.T) {
	r := newIRig(t)
	p := r.queue("a@example.org")
	r.flt.set(true, nil)
	_, err := r.svc.DeletePerson(ctxT(t), p)
	wantKind(t, err, errs.Unavailable)
	if !errors.Is(err, db.ErrReadOnly) {
		t.Fatalf("does not wrap db.ErrReadOnly: %v", err)
	}
}

// ---- purge -------------------------------------------------------------------------------

func TestPurge_RetentionDeletesOldFinalRowsOnly(t *testing.T) {
	r := newIRig(t)
	r.ret.set(notify.MinRetention)
	oldSent, newSent, oldFailed := r.queue("a@example.org"), r.queue("b@example.org"), r.queue("c@example.org")
	queued := r.queue("d@example.org")
	old := r.clk.Now().Add(-notify.MinRetention - time.Hour)
	recent := r.clk.Now().Add(-notify.MinRetention + time.Hour)
	r.exec("UPDATE core.notify_message SET status='sent', sent_at=$1, subject=NULL, body=NULL WHERE person_id=$2", old, [16]byte(oldSent))
	r.exec("UPDATE core.notify_message SET status='sent', sent_at=$1, subject=NULL, body=NULL WHERE person_id=$2", recent, [16]byte(newSent))
	r.exec("UPDATE core.notify_message SET status='failed', failed_at=$1, subject=NULL, body=NULL WHERE person_id=$2", old, [16]byte(oldFailed))
	r.exec("UPDATE core.notify_message SET created_at=$1 WHERE person_id=$2", old, [16]byte(queued))
	n, err := r.svc.Purge(ctxT(t))
	wantNoErr(t, err)
	if n != 2 {
		t.Fatalf("Purge = %d, want 2", n)
	}
	if len(r.rowsFor(oldSent)) != 0 || len(r.rowsFor(oldFailed)) != 0 {
		t.Error("old final rows survived")
	}
	if len(r.rowsFor(newSent)) != 1 || len(r.rowsFor(queued)) != 1 {
		t.Error("a recent or non-final row was purged")
	}
	if r.metric("bms.notify.purged", "", "") != 2 {
		t.Errorf("purged metric = %v", r.metric("bms.notify.purged", "", ""))
	}
}

func TestPurge_Forever_KeepsEverything_Real(t *testing.T) {
	r := newIRig(t)
	p := r.queue("a@example.org")
	r.exec("UPDATE core.notify_message SET status='sent', sent_at=$1, subject=NULL, body=NULL", r.clk.Now().Add(-1000*24*time.Hour))
	r.ret.set(0)
	n, err := r.svc.Purge(ctxT(t))
	wantNoErr(t, err)
	if n != 0 || len(r.rowsFor(p)) != 1 {
		t.Fatalf("Purge = %d", n)
	}
}

func TestPurge_ManyRows_LoopsUntilNone(t *testing.T) {
	r := newIRig(t)
	r.ret.set(notify.MinRetention)
	old := r.clk.Now().Add(-30 * 24 * time.Hour)
	r.exec(`INSERT INTO core.notify_message (id, owner, template, language, status, attempts, next_attempt_at, created_at, expires_at, sent_at)
		SELECT gen_random_uuid(), 'volunteers', 'volunteers.greet', 'en', 'sent', 1, $1::timestamptz, $1::timestamptz, $1::timestamptz + interval '1 day', $1::timestamptz FROM generate_series(1, 1005)`, old)
	n, err := r.svc.Purge(ctxT(t))
	wantNoErr(t, err)
	if n != 1005 || r.count() != 0 {
		t.Fatalf("Purge = %d, left %d", n, r.count())
	}
}

func TestPurge_ReadOnly_Real(t *testing.T) {
	r := newIRig(t)
	r.ret.set(notify.MinRetention)
	r.flt.set(true, nil)
	_, err := r.svc.Purge(ctxT(t))
	wantKind(t, err, errs.Unavailable)
	if !errors.Is(err, db.ErrReadOnly) {
		t.Fatalf("does not wrap db.ErrReadOnly: %v", err)
	}
}

// ---- Run ---------------------------------------------------------------------------------

// drive advances the fake clock one poll at a time until cond holds.
func (r *irig) drive(what string, cond func() bool) {
	r.t.Helper()
	r.driveBy(notify.DefaultPollInterval, what, cond)
}

func (r *irig) driveBy(step time.Duration, what string, cond func() bool) {
	r.t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			var msgs []string
			for _, rec := range r.cap.Records() {
				msgs = append(msgs, rec.Message)
			}
			r.t.Fatalf("timed out waiting for %s (clock %v, rows %d, envelopes %d, logs %v)", what, r.clk.Now(), r.count(), len(r.ch.envelopes()), msgs)
		}
		r.tickOnce(step, cond)
	}
}

// tickSpans counts finished sender ticks.
func (r *irig) tickSpans() int {
	n := 0
	for _, sp := range r.cap.Spans() {
		if sp.Name == "notify.tick" {
			n++
		}
	}
	return n
}

// tickOnce moves the fake clock one step and waits, by spinning, for the loop to finish
// the tick that step fires (or for cond). It moves the clock again only if the step was
// lost because the loop had not armed its ticker yet, so the clock never runs ahead of the
// work (a hot clock would age leases and rows past their limits).
func (r *irig) tickOnce(step time.Duration, cond func() bool) {
	r.t.Helper()
	before := r.tickSpans()
	ignore(r.clk.Advance(step))
	for until := time.Now().Add(250 * time.Millisecond); !cond() && r.tickSpans() == before && time.Now().Before(until); {
		runtimeYield()
	}
}

func TestRun_DeliversOnTick_AndStopsOnClose(t *testing.T) {
	r := newIRig(t)
	r.queue("a@example.org")
	ctx, cancel := context.WithCancel(ctxT(t))
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- r.svc.Run(ctx) }()
	r.drive("a delivery", func() bool { return len(r.ch.envelopes()) == 1 })
	wantNoErr(t, r.svc.Close(ctxT(t)))
	select {
	case err := <-done:
		wantNoErr(t, err)
	case <-time.After(10 * time.Second):
		t.Fatal("Run did not return after Close")
	}
}

func TestRun_SkipsWhileReadOnly_ThenDelivers(t *testing.T) {
	r := newIRig(t)
	r.queue("a@example.org")
	r.flt.set(true, nil)
	ctx, cancel := context.WithCancel(ctxT(t))
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- r.svc.Run(ctx) }()
	for i := 0; i < 5; i++ {
		r.tickOnce(notify.DefaultPollInterval, func() bool { return false })
	}
	if len(r.ch.envelopes()) != 0 {
		t.Fatal("delivered on a read-only copy")
	}
	r.flt.set(false, nil)
	r.drive("a delivery after the copy became writable", func() bool { return len(r.ch.envelopes()) == 1 })
	cancel()
	wantNoErr(t, <-done)
}

func TestRun_DatabaseDown_LogsOnceAndRetries(t *testing.T) {
	r := newIRig(t)
	r.queue("a@example.org")
	ticks := make(chan struct{}, 4096)
	r.flt.mu.Lock()
	r.flt.onTx = func() {
		select {
		case ticks <- struct{}{}:
		default:
		}
	}
	r.flt.mu.Unlock()
	r.flt.set(false, errs.E("test.store", errs.Unavailable, errors.New("database down")))
	ctx, cancel := context.WithCancel(ctxT(t))
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- r.svc.Run(ctx) }()
	for i := 0; i < 5; i++ {
		before := r.tickSpans()
		for r.tickSpans() == before {
			if ctx.Err() != nil {
				t.Fatalf("tick %d never finished", i)
			}
			r.tickOnce(notify.DefaultPollInterval, func() bool { return false })
		}
	}
	loud := 0
	for _, rec := range r.cap.Records() {
		if rec.Level >= 4 { // warn and above
			loud++
		}
	}
	if loud > 1 {
		t.Errorf("%d warn/error log lines for one outage across 5 ticks, want at most 1", loud)
	}
	r.flt.set(false, nil)
	r.drive("recovery", func() bool { return len(r.ch.envelopes()) == 1 })
	cancel()
	wantNoErr(t, <-done)
}

func TestRun_ExpiresStaleRows(t *testing.T) {
	r := newIRig(t)
	p := r.dbPerson("a@example.org")
	m := greet(p)
	m.TTL = time.Hour
	wantNoErr(t, r.vol.Send(ctxT(t), m))
	r.smtp.set(func(c *notify.SMTPConfig) { c.Host = "" }) // nothing is ever delivered
	ctx, cancel := context.WithCancel(ctxT(t))
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- r.svc.Run(ctx) }()
	r.driveBy(time.Hour, "the purge interval to expire the row", func() bool { return r.one(p).Status == "failed" })
	if x := r.one(p); x.Subject != nil || *x.LastErr != "expired" {
		t.Fatalf("row = %+v", x)
	}
	cancel()
	wantNoErr(t, <-done)
}

// ---- Mailpit end to end ------------------------------------------------------------------

func TestMailpit_EndToEnd_DeliveredOnce(t *testing.T) {
	addr := os.Getenv("BMS_TEST_SMTP_ADDR")
	api := os.Getenv("BMS_TEST_MAILPIT_URL")
	if addr == "" {
		t.Skip("BMS_TEST_SMTP_ADDR is not set; skipping the Mailpit end-to-end test")
	}
	if api == "" {
		t.Skip("BMS_TEST_MAILPIT_URL is not set (add BMS_TEST_MAILPIT_URL=http://localhost:8025 to deploy/compose/.env); skipping the Mailpit end-to-end test")
	}
	host, port, err := splitAddr(addr)
	wantNoErr(t, err)
	r := newIRig(t, func(o *notify.Options) { o.Channel = nil }) // the real smtp channel
	r.smtp.set(func(c *notify.SMTPConfig) { c.Host, c.Port, c.Security = host, port, notify.None })
	to := "e2e-" + strings.ReplaceAll(model.NewID[model.PersonID]().String(), "-", "") + "@example.org"
	p := r.dbPerson(to)
	wantNoErr(t, r.vol.Send(ctxT(t), greet(p)))
	wantNoErr(t, r.svc.Ping(ctxT(t)))
	if got := r.sendOnce(); got.Sent != 1 {
		t.Fatalf("Sent = %+v", got)
	}
	if got := r.sendOnce(); got.Sent != 0 {
		t.Fatalf("second tick: %+v", got)
	}
	mailpitExpectOne(t, api, to)
}

func TestRun_SweepsAtStart(t *testing.T) {
	r := newIRig(t)
	p := r.dbPerson("a@example.org")
	m := greet(p)
	m.TTL = time.Hour
	wantNoErr(t, r.vol.Send(ctxT(t), m))
	r.advance(2 * time.Hour)
	r.smtp.set(func(c *notify.SMTPConfig) { c.Host = "" })
	ctx, cancel := context.WithCancel(ctxT(t))
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- r.svc.Run(ctx) }()
	eventually(t, "the start-up sweep", func() bool { return r.one(p).Status == "failed" })
	if x := r.one(p); x.Subject != nil || *x.LastErr != "expired" {
		t.Fatalf("row = %+v", x)
	}
	cancel()
	wantNoErr(t, <-done)
}
