//go:build integration

package settings_test

import "testing"

// regional derives its ETag from the settings revision (regional.RevisionSource): Revision is
// the stored revision, and a write moves it.
func TestRevision_FollowsTheStoredRevisionAndEveryWrite(t *testing.T) {
	r := newIRig(t)
	ctx := ctxT(t)
	got, err := r.svc.Revision(ctx)
	wantNoErr(t, err)
	if int64(got) != r.dbRevision() {
		t.Errorf("Revision = %d, stored %d", got, r.dbRevision())
	}
	rev := r.mustSet("regional.name", "Camp Quill")
	after, err := r.svc.Revision(ctx)
	wantNoErr(t, err)
	if after != rev || after <= got {
		t.Errorf("Revision after a write = %d, Set answered %d, was %d", after, rev, got)
	}
}
