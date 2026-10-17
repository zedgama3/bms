package module_test

import (
	"fmt"
	"slices"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/zedgama3/bms/internal/core/module"
	"github.com/zedgama3/bms/internal/lib/errs"
)

func TestRegistryAdd_NilFactory_ReturnsInvalid(t *testing.T) {
	r := module.NewRegistry()
	wantKind(t, r.Add(mf("alpha"), nil), errs.Invalid)
	if got := r.Manifests(); len(got) != 0 {
		t.Fatalf("a failed Add registered %d modules", len(got))
	}
}

func TestRegistryAdd_InvalidManifest_ReturnsInvalid(t *testing.T) {
	r := module.NewRegistry()
	wantKind(t, r.Add(mf("Bad Name"), okFactory("x")), errs.Invalid)
	wantKind(t, r.Add(mf("core"), okFactory("core")), errs.Invalid)
	if got := r.Manifests(); len(got) != 0 {
		t.Fatalf("a failed Add registered %d modules", len(got))
	}
}

func TestRegistryAdd_DuplicateName_ReturnsConflict(t *testing.T) {
	r := module.NewRegistry()
	if err := r.Add(mf("alpha"), okFactory("alpha")); err != nil {
		t.Fatal(err)
	}
	wantKind(t, r.Add(mf("alpha", provides("x")), okFactory("alpha")), errs.Conflict)
	if got := r.Manifests(); len(got) != 1 || len(got[0].Provides) != 0 {
		t.Fatalf("the duplicate replaced or joined the first: %+v", got)
	}
}

func TestRegistryAdd_ThenManifests_SortedByName(t *testing.T) {
	r := simple(t, mf("gamma"), mf("alpha"), mf("delta"), mf("beta"))
	if got, want := names(r.Manifests()), []string{"alpha", "beta", "delta", "gamma"}; !slices.Equal(got, want) {
		t.Fatalf("Manifests() = %v, want %v", got, want)
	}
	if got := module.NewRegistry().Manifests(); len(got) != 0 {
		t.Fatalf("empty registry has manifests: %v", got)
	}
}

func TestRegistry_ConcurrentAdd(t *testing.T) {
	r := module.NewRegistry()
	var wg sync.WaitGroup
	for i := range 50 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			name := fmt.Sprintf("m%d", i)
			if err := r.Add(mf(name), okFactory(name)); err != nil {
				t.Errorf("Add(%s): %v", name, err)
			}
			r.Manifests()
		}()
	}
	wg.Wait()
	got := names(r.Manifests())
	if len(got) != 50 || !slices.IsSorted(got) {
		t.Fatalf("got %d manifests, sorted=%v", len(got), slices.IsSorted(got))
	}
}

func TestRegistry_ConcurrentAddSameName_ExactlyOneWins(t *testing.T) {
	r := module.NewRegistry()
	var wg sync.WaitGroup
	var ok, conflict atomic.Int32
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := r.Add(mf("alpha"), okFactory("alpha"))
			switch {
			case err == nil:
				ok.Add(1)
			case errs.Is(err, errs.Conflict):
				conflict.Add(1)
			default:
				t.Errorf("unexpected error: %v", err)
			}
		}()
	}
	wg.Wait()
	if ok.Load() != 1 || conflict.Load() != 19 {
		t.Fatalf("ok=%d conflict=%d, want 1 and 19", ok.Load(), conflict.Load())
	}
}

func TestRegister_AddsToDefault(t *testing.T) {
	const name = "registrytestfresh"
	module.Register(mf(name), okFactory(name))
	found := slices.Contains(names(module.Default().Manifests()), name)
	if !found {
		t.Fatalf("Default().Manifests() lacks %q", name)
	}
	if d1, d2 := module.Default(), module.Default(); d1 != d2 {
		t.Fatal("Default() must return the same registry every time")
	}
}

// TestRegister_RecordsProblem_ResolveReportsIt runs last among the tests that resolve Default:
// it poisons the process-wide registry on purpose.
func TestRegister_RecordsProblem_ResolveReportsIt(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Register panicked: %v", r)
		}
	}()
	module.Register(mf("Not A Valid Name"), okFactory("x")) // F12
	module.Register(mf("registrytestnilfactory"), nil)      // F11
	_, err := module.Default().Resolve(module.Env{})
	wantKind(t, err, errs.Invalid)
}

func TestRegister_DuplicateName_DoesNotPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Register panicked: %v", r)
		}
	}()
	module.Register(mf("registrytestdup"), okFactory("registrytestdup"))
	module.Register(mf("registrytestdup"), okFactory("registrytestdup"))
	if n := count(names(module.Default().Manifests()), "registrytestdup"); n != 1 {
		t.Fatalf("duplicate registered %d times", n)
	}
}

func count(s []string, v string) int {
	n := 0
	for _, x := range s {
		if x == v {
			n++
		}
	}
	return n
}
