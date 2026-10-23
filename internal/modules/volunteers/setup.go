package volunteers

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/zedgama3/bms/internal/contracts/eventsetup"
	"github.com/zedgama3/bms/internal/contracts/personpage"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
	"github.com/zedgama3/bms/internal/modules/volunteers/vol"
)

// chunked splits s into chunks of at most n.
func chunked[T any](s []T, n int) func(yield func([]T) bool) { return slices.Chunk(s, n) }

// Sections is the module's contribution to a person's page (people.person-page): section
// volunteers.summary with the departments the person belongs to (to the viewer's rosters.read
// extent; all of them for the person themself) and a count of upcoming shifts. A person the
// viewer may see nothing of is absent from the map. Fails errs.Unauthenticated and store
// kinds; people turns any error into a failed section.
func (s *Service) Sections(ctx context.Context, people []model.PersonID) (_ map[model.PersonID][]personpage.Section, err error) {
	const op = "volunteers.Sections"
	ctx, span := s.begin(ctx, "volunteers.sections")
	defer endSpan(span, &err)
	actor, err := s.actorOf(ctx, op)
	if err != nil {
		return nil, err
	}
	ext, err := s.extent(ctx, vol.PermissionRostersRead)
	if err != nil {
		return nil, wrap(op, err)
	}
	out := map[model.PersonID][]personpage.Section{}
	if len(people) == 0 {
		return out, nil
	}
	now := s.now()
	for chunk := range slices.Chunk(people, vol.MaxIDs) {
		mem, merr := s.store.MembershipsOfPeople(ctx, chunk, true, nil)
		if merr != nil {
			return nil, wrap(op, merr)
		}
		counts, cerr := s.store.UpcomingCounts(ctx, chunk, now, ext.Site, ext.Departments)
		if cerr != nil {
			return nil, wrap(op, cerr)
		}
		// self sees their own everything; counts for self need no extent.
		var selfCounts map[model.PersonID]int
		if slices.Contains(chunk, actor) {
			if selfCounts, cerr = s.store.UpcomingCounts(ctx, []model.PersonID{actor}, now, true, nil); cerr != nil {
				return nil, wrap(op, cerr)
			}
		}
		var ids []model.DepartmentID
		for _, m := range mem {
			ids = append(ids, m.Department)
		}
		names, nerr := s.deptNames(ctx, ids) // in chunks of vol.MaxIDs (F157)
		if nerr != nil {
			return nil, wrap(op, nerr)
		}
		shown := map[model.PersonID][]string{}
		for _, m := range mem {
			if m.Person == actor || ext.Covers(m.Department) {
				shown[m.Person] = append(shown[m.Person], names[m.Department])
			}
		}
		for _, p := range chunk {
			n := counts[p]
			if p == actor {
				n = selfCounts[actor]
			}
			if len(shown[p]) == 0 && n == 0 {
				continue
			}
			slices.Sort(shown[p])
			var items []personpage.Item
			if len(shown[p]) > 0 {
				items = append(items, personpage.Item{
					Label: "module.volunteers.section.summary.departments", Value: strings.Join(shown[p], ", "),
				})
			}
			upcoming := personpage.Item{Label: "module.volunteers.section.summary.upcoming", Value: strconv.Itoa(n)}
			if p == actor {
				upcoming.Href = "/volunteers/me"
			}
			items = append(items, upcoming)
			out[p] = []personpage.Section{{Key: "volunteers.summary", Items: items}}
		}
	}
	return out, nil
}

// SetUp is the module's event set-up contribution (events.event-setup): with CopyFrom set it
// copies the non-canceled shifts of every department where the caller holds shifts.manage
// from the earlier event to the new one, through CopyShifts. It is idempotent per
// (event, module) because a copied shift is never copied again. A caller who leads nothing
// gets Items 0 and no error. One department's failure is returned; the earlier departments'
// copies stand. Fails errs.Unauthenticated and the kinds of CopyShifts.
func (s *Service) SetUp(ctx context.Context, req eventsetup.Request) (_ eventsetup.Result, err error) {
	const op = "volunteers.SetUp"
	ctx, span := s.begin(ctx, "volunteers.set-up")
	defer endSpan(span, &err)
	actor, err := s.actorOf(ctx, op)
	if err != nil {
		return eventsetup.Result{}, err
	}
	if req.CopyFrom.IsZero() {
		return eventsetup.Result{}, nil
	}
	ext, err := s.extent(ctx, vol.PermissionShiftsManage)
	if err != nil {
		return eventsetup.Result{}, wrap(op, err)
	}
	if ext.IsZero() {
		return eventsetup.Result{}, nil
	}
	have, err := s.store.DepartmentsWithShifts(ctx, req.CopyFrom)
	if err != nil {
		return eventsetup.Result{}, wrap(op, err)
	}
	total := 0
	for _, d := range have {
		if !ext.Covers(d) {
			continue
		}
		n, cerr := s.copyShifts(ctx, actor, d, req.CopyFrom, req.Event, "setup")
		if cerr != nil {
			// A department archived since is not a failure of the others.
			if errs.Is(cerr, errs.Conflict) && vol.ConflictCode(cerr) == vol.ConflictArchived {
				continue
			}
			return eventsetup.Result{Items: total}, fmt.Errorf("%s: %w", op, cerr)
		}
		total += n
	}
	return eventsetup.Result{Items: total}, nil
}

// DepartmentsOf is the module's contribution to people.membership: the departments each
// person belongs to, whatever the viewer may see (people intersects the answer with the
// viewer's extent). A person with no membership is absent. Fails store kinds.
func (s *Service) DepartmentsOf(ctx context.Context, people []model.PersonID) (_ map[model.PersonID][]model.DepartmentID, err error) {
	const op = "volunteers.DepartmentsOf"
	ctx, span := s.begin(ctx, "volunteers.departments-of")
	defer endSpan(span, &err)
	out := map[model.PersonID][]model.DepartmentID{}
	for chunk := range slices.Chunk(people, vol.MaxIDs) {
		mem, merr := s.store.MembershipsOfPeople(ctx, chunk, true, nil)
		if merr != nil {
			return nil, wrap(op, merr)
		}
		for _, m := range mem {
			out[m.Person] = append(out[m.Person], m.Department)
		}
	}
	return out, nil
}
