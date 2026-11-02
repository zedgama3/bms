//go:build integration

package people_test

// The person page's sections (people.md F47, F48): a failing or blocking provider marks its
// section failed and the page still renders. Moved out of the data-rights file with the
// rest of the data rights; the module sources are datarights' now, so Link takes only the
// section and membership providers.

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/contracts/personpage"
	"github.com/zedgama3/bms/internal/core/people"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
)

func TestGet_ProviderFails_SectionMarkedFailed(t *testing.T) {
	r := newIRig(t, false)
	good := providerFunc(func(_ context.Context, ids []model.PersonID) (map[model.PersonID][]personpage.Section, error) {
		return map[model.PersonID][]personpage.Section{ids[0]: {{Key: "hello.greeting", Items: []personpage.Item{{Label: "module.hello.item.count", Value: "3"}}}}}, nil
	})
	bad := providerFunc(func(context.Context, []model.PersonID) (map[model.PersonID][]personpage.Section, error) {
		return nil, errs.E("camps.Sections", errs.Unavailable, errors.New("down"))
	})
	later := providerFunc(func(_ context.Context, ids []model.PersonID) (map[model.PersonID][]personpage.Section, error) {
		return map[model.PersonID][]personpage.Section{ids[0]: {{Key: "later.note"}}}, nil
	})
	wantNoErr(t, r.svc.Link([]people.SectionProvider{
		{Module: "hello", Provider: good}, {Module: "camps", Provider: bad}, {Module: "later", Provider: later},
	}, nil))
	me := r.insertPerson(seed{Playa: "Dusty Ember"})
	prof, err := r.svc.Me(asPerson(ctxT(t), me))
	wantNoErr(t, err)
	if len(prof.Sections) != 3 {
		t.Fatalf("sections = %+v, want three in build order", prof.Sections)
	}
	if prof.Sections[0].Key != "hello.greeting" || prof.Sections[0].Failed || len(prof.Sections[0].Items) != 1 {
		t.Errorf("section 0 = %+v", prof.Sections[0])
	}
	if prof.Sections[1].Key != "camps.unavailable" || !prof.Sections[1].Failed || len(prof.Sections[1].Items) != 0 {
		t.Errorf("section 1 = %+v, want camps.unavailable, Failed", prof.Sections[1])
	}
	if prof.Sections[2].Key != "later.note" || prof.Sections[2].Failed {
		t.Errorf("section 2 = %+v: a failing provider stopped the next", prof.Sections[2])
	}
	if got := r.measured("bms.people.sections", map[string]string{"module": "camps", "outcome": "failed"}); got != 1 {
		t.Errorf("bms.people.sections{camps,failed} = %v, want 1", got)
	}
}

// F48: a provider that blocks until its ctx ends is cut off after SectionTimeout and the
// page still renders, with a Failed section.
func TestGet_ProviderBlocks_SectionTimesOut(t *testing.T) {
	r := newIRig(t, false)
	entered := make(chan struct{})
	blocking := providerFunc(func(ctx context.Context, _ []model.PersonID) (map[model.PersonID][]personpage.Section, error) {
		close(entered)
		<-ctx.Done()
		return nil, errs.E("slow.Sections", errs.KindOf(ctx.Err()), ctx.Err())
	})
	wantNoErr(t, r.svc.Link([]people.SectionProvider{{Module: "slow", Provider: blocking}}, nil))
	me := r.insertPerson(seed{Playa: "Dusty Ember"})

	type result struct {
		prof people.Profile
		err  error
	}
	done := make(chan result, 1)
	go func() {
		prof, err := r.svc.Me(asPerson(ctxT(t), me))
		done <- result{prof, err}
	}()
	<-entered
	waitCtx, cancel := context.WithTimeout(ctxT(t), 15*time.Second)
	defer cancel()
	// If the service times a provider out on the injected clock, a timer is armed and we
	// pass SectionTimeout on it; if it uses a context deadline, nothing is armed and the
	// page returns after SectionTimeout of real time. Either way no test code sleeps.
	probe, stop := context.WithTimeout(waitCtx, 500*time.Millisecond)
	if err := r.clk.BlockUntil(probe, 1); err == nil {
		wantNoErr(t, r.clk.Advance(people.SectionTimeout))
	}
	stop()
	select {
	case res := <-done:
		wantNoErr(t, res.err)
		if len(res.prof.Sections) != 1 || !res.prof.Sections[0].Failed || res.prof.Sections[0].Key != "slow.unavailable" {
			t.Errorf("sections = %+v", res.prof.Sections)
		}
		if got := r.measured("bms.people.sections", map[string]string{"module": "slow", "outcome": "failed"}); got != 1 {
			t.Errorf("bms.people.sections{slow,failed} = %v, want 1", got)
		}
	case <-waitCtx.Done():
		t.Fatal("the page did not return after SectionTimeout passed on the injected clock")
	}
}

func TestGet_Providers_SeeTheViewerAndOnePerson(t *testing.T) {
	r := newIRig(t, false)
	target := r.insertPerson(seed{Playa: "Dusty Ember"})
	viewer, vctx := r.admin(permDirectory)
	var seenViewer model.PersonID
	var seenIDs []model.PersonID
	var calls int
	spy := providerFunc(func(ctx context.Context, ids []model.PersonID) (map[model.PersonID][]personpage.Section, error) {
		calls++
		seenIDs = ids
		seenViewer = viewerOf(ctx)
		return nil, nil // a person with nothing to show is simply absent
	})
	wantNoErr(t, r.svc.Link([]people.SectionProvider{{Module: "spy", Provider: spy}}, nil))
	prof, err := r.svc.Get(vctx, target)
	wantNoErr(t, err)
	if calls != 1 || len(seenIDs) != 1 || seenIDs[0] != target {
		t.Errorf("provider called %d times with %v, want once with the page's person", calls, seenIDs)
	}
	if seenViewer != viewer {
		t.Errorf("the provider saw viewer %v, want %v", seenViewer, viewer)
	}
	if len(prof.Sections) != 0 {
		t.Errorf("sections = %+v for a provider with nothing to show", prof.Sections)
	}
}

func TestSections_NotLinked_PageCarriesNone(t *testing.T) {
	r := newIRig(t, false)
	me := r.insertPerson(seed{Playa: "Dusty Ember"})
	prof, err := r.svc.Me(asPerson(ctxT(t), me))
	wantNoErr(t, err)
	if len(prof.Sections) != 0 {
		t.Errorf("sections before Link: %+v", prof.Sections)
	}
}
