package module

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/zedgama3/bms/internal/lib/errs"
)

// Built pairs a manifest with its module.
type Built struct {
	Manifest Manifest
	Module   Module
}

// ModuleRoutes is one module's routes and API description.
//
//nolint:revive // the name is fixed by module-contract.md
type ModuleRoutes struct {
	Module  string
	Routes  []Route
	OpenAPI []byte
}

// ModulePersonData pairs a module name with its PersonData.
//
//nolint:revive // the name is fixed by module-contract.md
type ModulePersonData struct {
	Module string
	PersonData
}

// setState is where a Set is in its life: new, started, or finished (stopped, or a Start
// that failed and was rolled back).
type setState uint8

const (
	setNew setState = iota
	setStarted
	setDone
)

// Set is the built, enabled modules. Built once per process; Start and Stop are each
// called once by app, not concurrently with each other. The accessors are safe for
// concurrent use.
type Set struct {
	built      []Built
	routes     []ModuleRoutes
	pd         []ModulePersonData
	contrib    map[ExtensionPoint][]Contribution
	corePoints map[ExtensionPoint]struct{}

	mu    sync.Mutex // guards state and serializes Start and Stop
	state setState
}

// Modules returns the built modules in build order. Cannot fail.
func (s *Set) Modules() []Built {
	out := make([]Built, len(s.built))
	for i, b := range s.built {
		out[i] = Built{Manifest: cloneManifest(b.Manifest), Module: b.Module}
	}
	return out
}

// Routes returns the validated routes of every RouteProvider, in build order, with each
// module's OpenAPI fragment, for api. Cannot fail.
func (s *Set) Routes() []ModuleRoutes {
	out := make([]ModuleRoutes, len(s.routes))
	for i, r := range s.routes {
		out[i] = ModuleRoutes{Module: r.Module, Routes: slices.Clone(r.Routes), OpenAPI: slices.Clone(r.OpenAPI)}
	}
	return out
}

// PersonData returns every module's PersonData with its name, in build order, for people
// (export and deletion run over all of them). Cannot fail.
func (s *Set) PersonData() []ModulePersonData { return slices.Clone(s.pd) }

// Contributors returns every enabled module's contribution to point, in build order, for
// core definers that are not modules and hold no Links (people for the person page,
// departments-events for new-event copying). Fails with errs.Invalid for any point not in
// Env.CoreExtensionPoints (F43b). Safe for concurrent use.
func (s *Set) Contributors(point ExtensionPoint) ([]Contribution, error) {
	const op = "module.Set.Contributors"
	if _, ok := s.corePoints[point]; !ok {
		return nil, invalid(op, "extension point %q is not one the core defines", point)
	}
	return append([]Contribution{}, s.contrib[point]...), nil
}

// StartRollbackTimeout bounds the rollback Stop calls when Start fails. The rollback runs
// on context.WithoutCancel(ctx) with this fresh deadline, because the failure may have
// spent the caller's start budget and every Stop would otherwise return Timeout at once,
// leaving the started goroutines unwaited (decision 20).
const StartRollbackTimeout = 10 * time.Second

// Start calls each module's Start in build order. If one fails, the modules already
// started are stopped in reverse order under StartRollbackTimeout (a fresh deadline on
// context.WithoutCancel(ctx)), and the error is returned wrapped with the failing
// module's name, joined with any Stop errors (F37). Fails with errs.Conflict if Start
// already succeeded (F38), and with a Canceled or Timeout error when ctx is done before a
// module is started.
func (s *Set) Start(ctx context.Context) error {
	const op = "module.Set.Start"
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state != setNew {
		return errs.E(op, errs.Conflict, errors.New("the set was already started"))
	}
	for i, b := range s.built {
		err := ctxErr(ctx, op)
		if err == nil {
			if err = b.Module.Start(ctx); err != nil {
				err = fmt.Errorf("module %s: start: %w", b.Manifest.Name, err)
			}
		}
		if err != nil {
			s.state = setDone
			rollback, cancel := context.WithTimeout(context.WithoutCancel(ctx), StartRollbackTimeout)
			defer cancel()
			return errors.Join(append([]error{err}, s.stopRange(rollback, i)...)...)
		}
	}
	s.state = setStarted
	return nil
}

// Stop calls each started module's Stop in reverse build order, continuing past
// failures, and returns them joined (F39). A Set that was never started, or already
// stopped, returns nil.
func (s *Set) Stop(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state != setStarted {
		return nil
	}
	s.state = setDone
	return errors.Join(s.stopRange(ctx, len(s.built))...)
}

// stopRange stops built[0:n] in reverse order and returns every failure.
func (s *Set) stopRange(ctx context.Context, n int) []error {
	var failed []error
	for i := n - 1; i >= 0; i-- {
		b := s.built[i]
		if err := b.Module.Stop(ctx); err != nil {
			failed = append(failed, fmt.Errorf("module %s: stop: %w", b.Manifest.Name, err))
		}
	}
	return failed
}
