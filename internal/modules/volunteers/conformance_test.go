package volunteers

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/core/audit"
	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/files"
	"github.com/zedgama3/bms/internal/core/module"
	"github.com/zedgama3/bms/internal/core/module/moduletest"
	"github.com/zedgama3/bms/internal/core/signals/signalstest"
	"github.com/zedgama3/bms/internal/model"
	"github.com/zedgama3/bms/internal/modules/volunteers/vol"
)

// The demo seed check (moduletest.WithDemoSeed) runs in the integration conformance test:
// the provider acts through the real use cases, which need real rows (fake_internal_test.go).
//
// TestConformance runs the module contract checks over an in-memory store for the person
// data paths: the seed books the subject into a department, a listing choice, a sign-up and a
// verified record with a certificate file.
func TestConformance(t *testing.T) {
	var (
		mu     sync.Mutex
		stores = map[*Service]*fakeStore{}
	)
	factory := func(_ context.Context, deps module.Deps) (module.Module, error) {
		st := newFakeStore()
		s, err := newService(deps, st, func(context.Context) (model.PersonID, bool) { return model.PersonID{}, false })
		if err != nil {
			return nil, err
		}
		mu.Lock()
		stores[s] = st
		mu.Unlock()
		return s, nil
	}
	provision := func(_ context.Context, m module.Manifest) (module.Deps, error) {
		sig, err := signalstest.NewFake(m.Name, m.Publishes, m.Subscribes)
		if err != nil {
			return module.Deps{}, fmt.Errorf("signalstest.NewFake: %w", err)
		}
		return module.Deps{Signals: sig}, nil
	}
	seed := func(ctx context.Context, mod module.Module, subject moduletest.Subject) error {
		s, ok := mod.(*Service)
		if !ok {
			return errors.New("the module under test is not a *Service")
		}
		mu.Lock()
		st := stores[s]
		mu.Unlock()
		if st == nil {
			return errors.New("no store for the module under test")
		}
		now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
		dept := model.NewID[model.DepartmentID]()
		rec := model.NewID[vol.RecordID]()
		staged, err := s.files.Stage(ctx, files.Upload{
			Name: "certificate.pdf", Accept: files.Documents, Body: bytes.NewReader([]byte("%PDF-1.4\n% seed\ntrailer\n%%EOF\n")),
		})
		if err != nil {
			return fmt.Errorf("stage: %w", err)
		}
		var certificate files.FileID
		err = s.db.Transact(ctx, db.Write, func(ctx context.Context) error {
			f, aerr := s.files.Attach(ctx, staged, files.Attachment{
				Record: ref(kindRecord, rec), Subject: subject.Person, Level: model.SensitivityPersonal,
			})
			certificate = f.ID
			return aerr //nolint:wrapcheck // test seed
		})
		if err != nil {
			return fmt.Errorf("attach: %w", err)
		}
		st.mu.Lock()
		defer st.mu.Unlock()
		st.members = append(st.members, vol.Member{ID: model.NewID[vol.MemberID](), Department: dept, Person: subject.Person, CreatedAt: now})
		st.unlisted[subject.Person] = true
		st.signups[subject.Person] = []vol.SignupExport{{
			Label: "Morning dirt", Department: dept, PositionName: "Dirt crew", Starts: now, Ends: now.Add(4 * time.Hour),
			Zone: "UTC", Status: vol.SignupBooked, BookedByRole: "self", CreatedAt: now,
		}}
		st.records[subject.Person] = []vol.RecordExport{{
			ID: rec, QualificationName: "Radio", AcquiredOn: vol.Date{Year: 2026, Month: 1, Day: 1}, Verified: true,
			VerifiedAt: now, Certificate: certificate,
		}}
		st.verified[subject.Person], st.created[subject.Person] = 1, 1
		return nil
	}
	moduletest.Conformance(t, Manifest, factory, provision, moduletest.WithSeed(seed))
}

var _ = audit.Succeeded
