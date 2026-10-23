package people

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/core/authz"
	"github.com/zedgama3/bms/internal/core/people/rows"
	"github.com/zedgama3/bms/internal/model"
)

// keyStore is a fakeStore that keeps the patches a use case writes.
type keyStore struct {
	*fakeStore
	upserts []rows.PersonPatch
	updates []rows.PersonPatch
}

func (k *keyStore) Upsert(ctx context.Context, p rows.PersonPatch, now time.Time) (rows.Person, bool, error) {
	k.upserts = append(k.upserts, p)
	return k.fakeStore.Upsert(ctx, p, now)
}

func (k *keyStore) UpdatePerson(_ context.Context, p rows.PersonPatch, now time.Time) (rows.Person, error) {
	k.updates = append(k.updates, p)
	return rows.Person{ID: p.ID, CreatedAt: now, UpdatedAt: now}, nil
}

// namedRig is a white-box rig whose actor already holds a playa name (and its stored key).
func namedRig(t *testing.T, name string, grants ...authz.Permission) (*wbRig, *keyStore) {
	t.Helper()
	r := newWB(t)
	for _, g := range grants {
		r.az.Grant(r.me, g)
	}
	r.st.person = rows.Person{ID: r.me, PlayaName: name, PlayaNameKey: NameKey(name)}
	ks := &keyStore{fakeStore: r.st}
	r.svc.store = ks
	return r, ks
}

func TestUpdateMe_FirstName_WritesTheKey_Unit(t *testing.T) {
	r, ks := namedRig(t, "")
	if _, err := r.svc.UpdateMe(r.ctx, ProfileInput{PlayaName: ptr("Dusty  Ember")}); err != nil {
		t.Fatalf("UpdateMe: %v", err)
	}
	if len(ks.upserts) != 1 || ks.upserts[0].PlayaNameKey == nil || *ks.upserts[0].PlayaNameKey != "dusty ember" {
		t.Fatalf("upserts = %+v, want one carrying the key %q", ks.upserts, "dusty ember")
	}
	if got := r.rec.Recorded(); len(got) != 1 || got[0].Reason != "" {
		t.Errorf("recorded %v, want one entry with no reason", got)
	}
}

func TestUpdateMe_SameName_NoOp_Unit(t *testing.T) {
	r, ks := namedRig(t, "Dusty Ember")
	if _, err := r.svc.UpdateMe(r.ctx, ProfileInput{PlayaName: ptr("Dusty Ember"), Pronouns: ptr("")}); err != nil {
		t.Fatalf("UpdateMe: %v", err)
	}
	if len(ks.upserts) != 0 || len(r.rec.Recorded()) != 0 {
		t.Errorf("wrote %v and audited %v for an unchanged profile", ks.upserts, r.actions())
	}
}

func TestUpdateMe_RespellSameKey_SavesAndAudits_Unit(t *testing.T) {
	r, ks := namedRig(t, "dusty ember")
	if _, err := r.svc.UpdateMe(r.ctx, ProfileInput{PlayaName: ptr("Dusty Ember")}); err != nil {
		t.Fatalf("UpdateMe: %v", err)
	}
	if len(ks.upserts) != 1 || *ks.upserts[0].PlayaName != "Dusty Ember" || *ks.upserts[0].PlayaNameKey != "dusty ember" {
		t.Fatalf("upserts = %+v", ks.upserts)
	}
	got := r.rec.Recorded()
	if len(got) != 1 || !slices.Equal(got[0].Fields, []string{FieldPlayaName}) || got[0].Reason != "" {
		t.Errorf("recorded %+v, want playa_name changed with no reason", got)
	}
}

func TestUpdateMe_LanguageOnly_SetNameUntouched_Unit(t *testing.T) {
	r, ks := namedRig(t, "Dusty Ember")
	if _, err := r.svc.UpdateMe(r.ctx, ProfileInput{Language: ptr("en")}); err != nil {
		t.Fatalf("UpdateMe: %v", err)
	}
	if len(ks.upserts) != 1 || ks.upserts[0].PlayaName != nil || ks.upserts[0].PlayaNameKey != nil {
		t.Errorf("upserts = %+v, want a language-only patch", ks.upserts)
	}
}

func TestUpdate_OtherPersonRename_SavesAndAuditsReason_Unit(t *testing.T) {
	r, ks := namedRig(t, "Dusty Ember", PermissionProfilesManage)
	other := model.NewID[model.PersonID]()
	r.st.person = rows.Person{ID: other, PlayaName: "Rude Name", PlayaNameKey: NameKey("Rude Name")}
	if _, err := r.svc.Update(r.ctx, other, ProfileInput{PlayaName: ptr("Ember Glow")}); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if len(ks.updates) != 1 || *ks.updates[0].PlayaNameKey != "ember glow" {
		t.Fatalf("updates = %+v", ks.updates)
	}
	got := r.rec.Recorded()
	if len(got) != 1 || got[0].Reason != "rename" || !slices.Equal(got[0].Fields, []string{FieldPlayaName}) {
		t.Errorf("recorded %+v, want playa_name changed with Reason rename", got)
	}
}

func TestUpdate_FirstNameByManager_HasNoReason_Unit(t *testing.T) {
	r, ks := namedRig(t, "Dusty Ember", PermissionProfilesManage)
	other := model.NewID[model.PersonID]()
	r.st.person = rows.Person{ID: other}
	if _, err := r.svc.Update(r.ctx, other, ProfileInput{PlayaName: ptr("Ember Glow")}); err != nil {
		t.Fatalf("Update: %v", err)
	}
	got := r.rec.Recorded()
	if len(ks.updates) != 1 || len(got) != 1 || got[0].Reason != "" {
		t.Errorf("updates %+v, recorded %+v, want a write with no reason", ks.updates, got)
	}
}

func TestUpdate_RenameOwnNameByManager_SavesAndAuditsReason_Unit(t *testing.T) {
	r, ks := namedRig(t, "Dusty Ember", PermissionProfilesManage)
	if _, err := r.svc.Update(r.ctx, r.me, ProfileInput{PlayaName: ptr("Ember Glow")}); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if len(ks.upserts) != 1 || *ks.upserts[0].PlayaNameKey != "ember glow" {
		t.Fatalf("upserts = %+v", ks.upserts)
	}
	got := r.rec.Recorded()
	if len(got) != 1 || got[0].Reason != "rename" || !slices.Equal(got[0].Fields, []string{FieldPlayaName}) {
		t.Errorf("recorded %+v, want playa_name changed with Reason rename", got)
	}
}
