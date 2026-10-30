package jobs_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/zedgama3/bms/internal/core/db/dbtest"
)

// Decision 25 (J-W1): StatusView.Instance is Options.Instance, and a store with no rows
// answers four empty lists and not an error (needs no database).
func TestStatus_NoRows_InstanceSetAndListsEmpty(t *testing.T) {
	r := newRig(t, dbtest.NewFake())
	v, err := r.run.Status(r.holder(permRead))
	wantNoErr(t, err)
	if v.Instance != "test-a" {
		t.Errorf("Instance = %q, want %q", v.Instance, "test-a")
	}
	if len(v.Failed)+len(v.Running)+len(v.Retrying)+len(v.Waiting) != 0 {
		t.Errorf("lists not empty: %+v", v)
	}
}

// Decision 25 wire: {read_only, instance, registered, counts, failed, running, retrying,
// waiting}; "recent" is gone; the four lists are arrays, never null.
func TestRoutes_Status_WireHasFourListsAndNoRecent(t *testing.T) {
	r := adminRig(t)
	w := serve(r.holder(permRead), t, r, "GET", "/")
	if w.Code != http.StatusOK {
		t.Fatalf("GET / = %d: %s", w.Code, w.Body.String())
	}
	var m map[string]json.RawMessage
	wantNoErr(t, json.Unmarshal(w.Body.Bytes(), &m))
	for _, k := range []string{"read_only", "instance", "registered", "counts", "failed", "running", "retrying", "waiting"} {
		if _, ok := m[k]; !ok {
			t.Errorf("response lacks %q", k)
		}
	}
	if _, ok := m["recent"]; ok {
		t.Error("response still carries recent")
	}
	for _, k := range []string{"failed", "running", "retrying", "waiting"} {
		if got := string(m[k]); got != "[]" {
			t.Errorf("%s = %s on an empty queue, want []", k, got)
		}
	}
	var inst string
	wantNoErr(t, json.Unmarshal(m["instance"], &inst))
	if inst != "test-a" {
		t.Errorf("instance = %q", inst)
	}
}

// The OpenAPI fragment describes the four arrays and not "recent".
func TestOpenAPI_Status_FourListsNoRecent(t *testing.T) {
	r := adminRig(t)
	frag := string(r.run.OpenAPI())
	for _, k := range []string{`"failed"`, `"running"`, `"retrying"`, `"waiting"`, `"instance"`} {
		if !strings.Contains(frag, k) {
			t.Errorf("fragment lacks %s", k)
		}
	}
	if strings.Contains(frag, `"recent"`) {
		t.Error("fragment still describes recent")
	}
}
