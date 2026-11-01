package files_test

import (
	"strings"
	"testing"

	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/files"
	"github.com/zedgama3/bms/internal/core/objects/objectstest"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/validate"
)

func baseOptions(t *testing.T) files.Options {
	t.Helper()
	return newRig(t).opts
}

// F1
func TestNew_MissingDependency_ReturnsInvalid(t *testing.T) {
	tests := []struct {
		field string
		mut   func(*files.Options)
	}{
		{"files.db", func(o *files.Options) { o.DB = nil }},
		{"files.clock", func(o *files.Options) { o.Clock = nil }},
		{"files.logger", func(o *files.Options) { o.Logger = nil }},
		{"files.tracer", func(o *files.Options) { o.Tracer = nil }},
		{"files.audit", func(o *files.Options) { o.Audit = nil }},
		{"files.actor", func(o *files.Options) { o.Actor = nil }},
		{"files.objects", func(o *files.Options) { o.Objects = nil }},
		{"files.temp_dir", func(o *files.Options) { o.TempDir = "" }},
	}
	for _, tc := range tests {
		t.Run(tc.field, func(t *testing.T) {
			o := baseOptions(t)
			tc.mut(&o)
			err := o.Validate()
			if !errs.Is(err, errs.Invalid) {
				t.Fatalf("Validate = %v, want Invalid", err)
			}
			found := false
			for _, fe := range fieldsOf(err) {
				if fe == tc.field {
					found = true
				}
			}
			if !found {
				t.Errorf("Validate did not name %s (named %v)", tc.field, fieldsOf(err))
			}
			svc, err := files.New(o)
			if !errs.Is(err, errs.Invalid) || svc != nil {
				t.Errorf("New = %v, %v; want nil, Invalid", svc, err)
			}
		})
	}
}

func TestNew_AllMissing_NamesEveryField(t *testing.T) {
	err := (files.Options{}).Validate()
	wantKind(t, err, errs.Invalid)
	named := strings.Join(fieldsOf(err), ",")
	for _, f := range []string{"files.db", "files.clock", "files.logger", "files.tracer", "files.audit", "files.actor", "files.objects", "files.temp_dir"} {
		if !strings.Contains(named, f) {
			t.Errorf("missing %s in %s", f, named)
		}
	}
}

func TestNew_NilMeter_IsAllowed(t *testing.T) {
	o := baseOptions(t)
	o.Meter = nil
	if _, err := files.New(o); err != nil {
		t.Fatalf("New(nil Meter) = %v", err)
	}
}

// boundaries step 5: Settings (the reader scoped "files") is required.
func TestNew_NilSettings_ReturnsInvalid(t *testing.T) {
	o := baseOptions(t)
	o.Settings = nil
	svc, err := files.New(o)
	wantKind(t, err, errs.Invalid)
	if svc != nil {
		t.Error("New returned a service with its error")
	}
	wantKind(t, o.Validate(), errs.Invalid)
}

// "New does no I/O, so bms openapi and bms check build against db.None()".
func TestNew_DoesNoIO(t *testing.T) {
	o := baseOptions(t)
	o.DB = db.None()
	broken := newFault(objectstest.NewMem())
	broken.down()
	o.Objects = broken
	svc, err := files.New(o)
	if err != nil {
		t.Fatalf("New over db.None and a dead store: %v", err)
	}
	if _, err := svc.Scoped(ownerVol); err != nil {
		t.Errorf("Scoped over db.None: %v", err)
	}
	for _, op := range []string{"put", "get", "head", "delete", "list", "ping"} {
		if n := broken.count(op); n != 0 {
			t.Errorf("New/Scoped called store.%s %d times", op, n)
		}
	}
}

// F2
func TestScoped_BadOwner_ReturnsInvalid(t *testing.T) {
	r := newRig(t)
	for _, owner := range []string{"", "Volunteers", "1volunteers", "vol-unteers", "vol_unteers", "a.b", "a/b", " a", strings.Repeat("a", 33)} {
		st, err := r.svc.Scoped(owner)
		if !errs.Is(err, errs.Invalid) {
			t.Errorf("Scoped(%q) = %v, want Invalid", owner, err)
		}
		if st != nil {
			t.Errorf("Scoped(%q) returned a Store with its error", owner)
		}
	}
	for _, owner := range []string{"volunteers", "settings", "a", "afterreports"} {
		if _, err := r.svc.Scoped(owner); err != nil {
			t.Errorf("Scoped(%q) = %v", owner, err)
		}
	}
}

func TestScoped_SameOwnerTwice_SharesTheBucket(t *testing.T) {
	r := newRig(t)
	a, err := r.svc.Scoped(ownerVol)
	wantNoErr(t, err)
	st := mustStage(t, r.vol, "a.png", files.Images, plainPNG(t))
	// A second handle for the same owner writes to the same bucket.
	st2 := mustStage(t, a, "b.png", files.Images, plainPNG(t))
	if st.Bucket != st2.Bucket {
		t.Errorf("buckets differ: %q vs %q", st.Bucket, st2.Bucket)
	}
}

func fieldsOf(err error) []string {
	var out []string
	for _, fe := range validate.Fields(err) {
		out = append(out, fe.Field)
	}
	return out
}
