package authz

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/core/authz/rows"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
)

// holdersStore answers PermissionHolders and the per-person rules of HoldersOf.
type holdersStore struct {
	store
	holders  []model.PersonID
	rulesFor map[model.PersonID][]rows.HeldRule
	holdErr  error
	rulesErr error
	asked    string
}

func (h *holdersStore) PermissionHolders(_ context.Context, p string, _ time.Time) ([]model.PersonID, error) {
	h.asked = p
	return h.holders, h.holdErr
}

func (h *holdersStore) RulesHeldBy(_ context.Context, person model.PersonID, _ time.Time) ([]rows.HeldRule, error) {
	return h.rulesFor[person], h.rulesErr
}

func personIDs(n int) []model.PersonID {
	out := make([]model.PersonID, n)
	for i := range out {
		out[i] = model.NewID[model.PersonID]()
	}
	slices.SortFunc(out, func(a, b model.PersonID) int { return strings.Compare(a.String(), b.String()) })
	return out
}

// F68 (rule part, unit): a rule forbidding p, by permission or by level, drops its holder;
// a rule about another permission does not; the result is in person ID order.
func TestHoldersOf_RulesDropHolders(t *testing.T) {
	ids := personIDs(5)
	st := &holdersStore{
		holders: []model.PersonID{ids[3], ids[0], ids[1], ids[2], ids[4]}, // out of order on purpose
		rulesFor: map[model.PersonID][]rows.HeldRule{
			ids[1]: {{Permission: string(pMedRead)}},   // forbids p
			ids[2]: {{Permission: string(pShiftRead)}}, // another permission
			ids[3]: {{Reaches: "sensitive"}},           // a level reaching p
			ids[4]: {{Reaches: "internal"}},            // a level reaching p, and more
		},
	}
	s := newTestService(t, st)
	got, err := s.HoldersOf(t.Context(), pMedRead)
	if err != nil {
		t.Fatalf("HoldersOf: %v", err)
	}
	want := []model.PersonID{ids[0], ids[2]}
	if !slices.Equal(got, want) {
		t.Errorf("HoldersOf = %v, want %v", got, want)
	}
	if st.asked != string(pMedRead) {
		t.Errorf("store asked about %q, want %q", st.asked, pMedRead)
	}
}

func TestHoldersOf_NoCandidates_ReturnsEmptyNotNil(t *testing.T) {
	s := newTestService(t, &holdersStore{})
	got, err := s.HoldersOf(t.Context(), pMedRead)
	if err != nil || got == nil || len(got) != 0 {
		t.Errorf("HoldersOf = %#v, %v; want an empty non-nil slice", got, err)
	}
}

func TestHoldersOf_BrokenRule_ReturnsInternal(t *testing.T) {
	ids := personIDs(1)
	s := newTestService(t, &holdersStore{holders: ids, rulesFor: map[model.PersonID][]rows.HeldRule{ids[0]: {{Reaches: "no such level"}}}})
	if _, err := s.HoldersOf(t.Context(), pMedRead); !errs.Is(err, errs.Internal) {
		t.Errorf("HoldersOf with an unreadable rule = %v, want Internal (fail closed)", err)
	}
}

// F69: the store's kinds come out unchanged, from either query.
func TestHoldersOf_StoreFails_ReturnsKind(t *testing.T) {
	ids := personIDs(1)
	for _, tc := range []struct {
		name  string
		err   error
		rules bool // fail the rules query instead of the holders query
		kind  errs.Kind
	}{
		{"unavailable", errs.E("test", errs.Unavailable, errors.New("down")), false, errs.Unavailable},
		{"timeout", errs.E("test", errs.Timeout, errors.New("slow")), false, errs.Timeout},
		{"canceled", errs.E("test", errs.Canceled, context.Canceled), false, errs.Canceled},
		{"rules unavailable", errs.E("test", errs.Unavailable, errors.New("down")), true, errs.Unavailable},
		{"rules timeout", errs.E("test", errs.Timeout, errors.New("slow")), true, errs.Timeout},
	} {
		t.Run(tc.name, func(t *testing.T) {
			st := &holdersStore{holders: ids}
			if tc.rules {
				st.rulesErr = tc.err
			} else {
				st.holdErr = tc.err
			}
			s := newTestService(t, st)
			got, err := s.HoldersOf(t.Context(), pMedRead)
			if !errs.Is(err, tc.kind) {
				t.Errorf("HoldersOf = %v, want kind %v", err, tc.kind)
			}
			if got != nil {
				t.Errorf("HoldersOf returned %v with an error", got)
			}
		})
	}
}
