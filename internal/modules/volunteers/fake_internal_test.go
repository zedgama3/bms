package volunteers

import (
	"context"
	"sync"

	"github.com/zedgama3/bms/internal/model"
	"github.com/zedgama3/bms/internal/modules/volunteers/vol"
)

// fakeStore is an in-memory store for the person-data paths (export, delete) and the
// conformance seed. It embeds the store interface so a method it does not implement panics
// on a nil pointer: a unit test that needs rows beyond these uses the real database.
type fakeStore struct {
	store

	mu        sync.Mutex
	members   []vol.Member
	unlisted  map[model.PersonID]bool
	signups   map[model.PersonID][]vol.SignupExport
	records   map[model.PersonID][]vol.RecordExport
	verified  map[model.PersonID]int
	created   map[model.PersonID]int
	failOn    map[string]error
	anonymise map[model.PersonID]vol.Anonymised
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		unlisted: map[model.PersonID]bool{}, signups: map[model.PersonID][]vol.SignupExport{},
		records: map[model.PersonID][]vol.RecordExport{}, verified: map[model.PersonID]int{},
		created: map[model.PersonID]int{}, failOn: map[string]error{}, anonymise: map[model.PersonID]vol.Anonymised{},
	}
}

func (f *fakeStore) fail(method string) error { return f.failOn[method] }

func (f *fakeStore) MembershipsOf(_ context.Context, p model.PersonID) ([]vol.Member, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.fail("MembershipsOf"); err != nil {
		return nil, err
	}
	var out []vol.Member
	for _, m := range f.members {
		if m.Person == p {
			out = append(out, m)
		}
	}
	return out, nil
}

func (f *fakeStore) MembershipsOfPeople(_ context.Context, people []model.PersonID, _ bool, _ []model.DepartmentID) ([]vol.Member, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []vol.Member
	for _, m := range f.members {
		for _, p := range people {
			if m.Person == p {
				out = append(out, m)
			}
		}
	}
	return out, nil
}

func (f *fakeStore) Listed(_ context.Context, p model.PersonID) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return !f.unlisted[p], nil
}

func (f *fakeStore) ExportSignups(_ context.Context, p model.PersonID) ([]vol.SignupExport, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]vol.SignupExport(nil), f.signups[p]...), nil
}

func (f *fakeStore) ExportRecords(_ context.Context, p model.PersonID) ([]vol.RecordExport, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]vol.RecordExport(nil), f.records[p]...), nil
}

func (f *fakeStore) CountVerifiedBy(_ context.Context, p model.PersonID) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.verified[p], nil
}

func (f *fakeStore) CountShiftsCreatedBy(_ context.Context, p model.PersonID) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.created[p], nil
}

func (f *fakeStore) RecordIDsOf(_ context.Context, p model.PersonID) ([]vol.ExpiredRecord, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.fail("RecordIDsOf"); err != nil {
		return nil, err
	}
	var out []vol.ExpiredRecord
	for _, r := range f.records[p] {
		out = append(out, vol.ExpiredRecord{ID: r.ID, Certificate: r.Certificate})
	}
	return out, nil
}

func (f *fakeStore) DeleteSignupsByPerson(_ context.Context, p model.PersonID) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := len(f.signups[p])
	delete(f.signups, p)
	return n, nil
}

func (f *fakeStore) DeleteMembersByPerson(_ context.Context, p model.PersonID) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	kept, n := f.members[:0:0], 0
	for _, m := range f.members {
		if m.Person == p {
			n++
			continue
		}
		kept = append(kept, m)
	}
	f.members = kept
	return n, nil
}

func (f *fakeStore) DeleteUnlistedByPerson(_ context.Context, p model.PersonID) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.unlisted[p] {
		return 0, nil
	}
	delete(f.unlisted, p)
	return 1, nil
}

func (f *fakeStore) DeleteRecordsByPerson(_ context.Context, p model.PersonID) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := len(f.records[p])
	delete(f.records, p)
	return n, nil
}

func (f *fakeStore) AnonymisePerson(_ context.Context, p model.PersonID) (vol.Anonymised, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	a := vol.Anonymised{Records: f.verified[p], Shifts: f.created[p]}
	delete(f.verified, p)
	delete(f.created, p)
	return a, nil
}
