package events_test

import (
	"context"
	"encoding/json"
	"go/build"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/core/authz"
	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/db/dbtest"
	"github.com/zedgama3/bms/internal/core/events"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
)

// unit is a linked rig over a dbtest.Fake: it proves what happens before the store is reached.
type unit struct {
	*rig
	fake *dbtest.Fake
}

func newUnit(t *testing.T) *unit {
	t.Helper()
	f := dbtest.NewFake()
	r := newRig(t, f)
	r.link()
	return &unit{rig: r, fake: f}
}

func (u *unit) noStatements(t *testing.T, what string) {
	t.Helper()
	if n := len(u.fake.Calls()); n != 0 {
		t.Errorf("%d statements for %s", n, what)
	}
	if len(u.rec.Recorded()) != 0 {
		t.Errorf("audit entries %v for %s", actionsOf(u.rec.Recorded()), what)
	}
	if len(u.bus.Published()) != 0 {
		t.Errorf("signals published for %s", what)
	}
}

func (u *unit) manager() (context.Context, model.PersonID) {
	me := newPerson()
	u.az.Grant(me, events.PermissionManage)
	return asPerson(ctxT(u.t), me), me
}

// ---- New and Options (F40) -------------------------------------------------------------

func TestNew_BadOptions_ReturnsInvalid(t *testing.T) {
	tests := []struct {
		field string
		mod   func(*events.Options)
	}{
		{"events.db", func(o *events.Options) { o.DB = nil }},
		{"events.clock", func(o *events.Options) { o.Clock = nil }},
		{"events.logger", func(o *events.Options) { o.Logger = nil }},
		{"events.tracer", func(o *events.Options) { o.Tracer = nil }},
		{"events.meter", func(o *events.Options) { o.Meter = nil }},
		{"events.audit", func(o *events.Options) { o.Audit = nil }},
		{"events.signals", func(o *events.Options) { o.Signals = nil }},
		{"events.authz", func(o *events.Options) { o.Authz = nil }},
		{"events.actor", func(o *events.Options) { o.Actor = nil }},
		{"events.setup_timeout", func(o *events.Options) { o.SetupTimeout = 999 * time.Millisecond }},
		{"events.setup_timeout", func(o *events.Options) { o.SetupTimeout = 5*time.Minute + time.Nanosecond }},
		{"events.setup_timeout", func(o *events.Options) { o.SetupTimeout = -time.Second }},
		{"events.setup_timeout", func(o *events.Options) { o.SetupTimeout = time.Nanosecond }},
	}
	for _, tc := range tests {
		t.Run(tc.field, func(t *testing.T) {
			o, _ := validOptions(t, dbtest.NewFake())
			tc.mod(&o)
			wantField(t, o.Validate(), tc.field, "")
			svc, err := events.New(o)
			wantField(t, err, tc.field, "")
			if svc != nil {
				t.Error("New returned a service with its error")
			}
		})
	}
}

func TestOptions_Validate_Table(t *testing.T) {
	ok := map[string]time.Duration{
		"zero is the default": 0,
		"lower bound":         time.Second,
		"upper bound":         5 * time.Minute,
		"middle":              30 * time.Second,
		"the default":         events.DefaultSetupTimeout,
	}
	for name, d := range ok {
		t.Run(name, func(t *testing.T) {
			o, _ := validOptions(t, dbtest.NewFake())
			o.SetupTimeout = d
			wantNoErr(t, o.Validate())
			if _, err := events.New(o); err != nil {
				t.Fatalf("New: %v", err)
			}
		})
	}
	all := events.Options{}
	err := all.Validate()
	wantKind(t, err, errs.Invalid)
	for _, f := range []string{"db", "clock", "logger", "tracer", "meter", "audit", "signals", "authz", "actor"} {
		if !hasField(err, "events."+f, "") {
			t.Errorf("zero Options does not report events.%s", f)
		}
	}
}

func TestNew_AgainstNoDatabase_Succeeds(t *testing.T) {
	o, _ := validOptions(t, db.None())
	if _, err := events.New(o); err != nil {
		t.Fatalf("New against db.None(): %v", err)
	}
}

// ---- Link (F78) ------------------------------------------------------------------------

func TestLink_Twice_ReturnsInternal(t *testing.T) {
	r := newRig(t, dbtest.NewFake())
	r.link(provider("alpha", nil, 1).sp())
	wantKind(t, r.svc.Link([]events.SetupProvider{provider("beta", nil, 1).sp()}), errs.Internal)
	wantKind(t, r.svc.Link(nil), errs.Internal)
}

func TestLink_BadProvider_ReturnsInternal(t *testing.T) {
	good := provider("alpha", nil, 1)
	tests := map[string][]events.SetupProvider{
		"nil provider":     {{Module: "alpha"}},
		"empty module":     {{Module: "", Provider: good}},
		"duplicate module": {{Module: "alpha", Provider: good}, {Module: "alpha", Provider: provider("alpha", nil, 1)}},
		"nil after a good": {{Module: "alpha", Provider: good}, {Module: "beta"}},
	}
	for name, ps := range tests {
		t.Run(name, func(t *testing.T) {
			r := newRig(t, dbtest.NewFake())
			wantKind(t, r.svc.Link(ps), errs.Internal)
		})
	}
}

func TestLink_NoProviders_Succeeds(t *testing.T) {
	r := newRig(t, dbtest.NewFake())
	wantNoErr(t, r.svc.Link(nil))
}

// ---- no actor (F41) --------------------------------------------------------------------

type useCase struct {
	name string
	call func(ctx context.Context, s *events.Service) error
}

func allUseCases() []useCase {
	ev, item := newEvent(), model.NewID[events.ScheduleItemID]()
	other := newEvent()
	st := epoch.AddDate(0, 0, 31)
	return []useCase{
		{"List", func(ctx context.Context, s *events.Service) error {
			_, err := s.List(ctx, events.ListQuery{})
			return keep(err)
		}},
		{"Get", func(ctx context.Context, s *events.Service) error { _, err := s.Get(ctx, ev); return keep(err) }},
		{"Create", func(ctx context.Context, s *events.Service) error {
			_, err := s.Create(ctx, validCreate("Burn 2027"))
			return keep(err)
		}},
		{"Update", func(ctx context.Context, s *events.Service) error {
			_, err := s.Update(ctx, ev, events.UpdateInput{Name: str("Renamed")})
			return keep(err)
		}},
		{"Close", func(ctx context.Context, s *events.Service) error { _, err := s.Close(ctx, ev); return keep(err) }},
		{"Reopen", func(ctx context.Context, s *events.Service) error { _, err := s.Reopen(ctx, ev); return keep(err) }},
		{"RunSetup", func(ctx context.Context, s *events.Service) error {
			_, err := s.RunSetup(ctx, ev, other)
			return keep(err)
		}},
		{"ListKinds", func(ctx context.Context, s *events.Service) error { _, err := s.ListKinds(ctx); return keep(err) }},
		{"CreateKind", func(ctx context.Context, s *events.Service) error {
			_, err := s.CreateKind(ctx, events.KindInput{Key: "work-weekend", Name: "Work weekend"})
			return keep(err)
		}},
		{"UpdateKind", func(ctx context.Context, s *events.Service) error {
			_, err := s.UpdateKind(ctx, "burn", events.KindInput{Name: "Burn!"})
			return keep(err)
		}},
		{"AddScheduleItem", func(ctx context.Context, s *events.Service) error {
			_, err := s.AddScheduleItem(ctx, ev, events.ScheduleInput{Label: "Gate", Starts: st})
			return keep(err)
		}},
		{"UpdateScheduleItem", func(ctx context.Context, s *events.Service) error {
			_, err := s.UpdateScheduleItem(ctx, ev, item, events.ScheduleInput{Label: "Gate", Starts: st})
			return keep(err)
		}},
		{"RemoveScheduleItem", func(ctx context.Context, s *events.Service) error { return s.RemoveScheduleItem(ctx, ev, item) }},
	}
}

func TestUseCases_NoActor_ReturnsUnauthenticated(t *testing.T) {
	contexts := map[string]func(testing.TB) context.Context{
		"no session":         func(t testing.TB) context.Context { return ctxT(t) },
		"restricted session": func(t testing.TB) context.Context { return asRestricted(ctxT(t), newPerson()) },
		"signal delivery":    func(t testing.TB) context.Context { return inDelivery(t) },
	}
	for cname, mk := range contexts {
		for _, uc := range allUseCases() {
			t.Run(cname+"/"+uc.name, func(t *testing.T) {
				u := newUnit(t)
				wantKind(t, uc.call(mk(t), u.svc), errs.Unauthenticated)
				u.noStatements(t, "an unauthenticated call")
			})
		}
	}
}

func TestInfoOf_ServesTheSystem(t *testing.T) {
	u := newUnit(t)
	for name, ctx := range map[string]context.Context{"no session": ctxT(t), "signal delivery": inDelivery(t)} {
		t.Run(name, func(t *testing.T) {
			got, err := u.svc.InfoOf(ctx, []model.EventID{newEvent()})
			wantNoErr(t, err)
			if len(got) != 0 {
				t.Errorf("InfoOf of an unknown event = %v", got)
			}
		})
	}
}

// ---- permissions (F44, F61, F63, F69, F75) ---------------------------------------------

// Reads need a session and no permission (decision 2).
func TestReads_SignedInWithoutPermission_Allowed(t *testing.T) {
	u := newUnit(t)
	ctx := asPerson(ctxT(t), newPerson())
	if _, err := u.svc.List(ctx, events.ListQuery{}); err != nil {
		t.Errorf("List: %v", err)
	}
	if _, err := u.svc.ListKinds(ctx); err != nil {
		t.Errorf("ListKinds: %v", err)
	}
	if _, err := u.svc.List(ctx, events.ListQuery{Status: events.StatusClosed}); err != nil {
		t.Errorf("List(closed): %v", err)
	}
}

func TestWrites_WithoutManage_ReturnsPermission(t *testing.T) {
	writes := map[string]bool{
		"Create": true, "Update": true, "Close": true, "Reopen": true, "RunSetup": true,
		"CreateKind": true, "UpdateKind": true,
		"AddScheduleItem": true, "UpdateScheduleItem": true, "RemoveScheduleItem": true,
	}
	grants := map[string]func(r *rig, p model.PersonID){
		"no grants":                   func(*rig, model.PersonID) {},
		"manage at a department only": func(r *rig, p model.PersonID) { r.az.GrantIn(p, events.PermissionManage, newDept()) },
		"departments.manage":          func(r *rig, p model.PersonID) { r.az.Grant(p, "departments.manage") },
		"departments.details.manage":  func(r *rig, p model.PersonID) { r.az.Grant(p, "departments.details.manage") },
		"people.profiles.manage":      func(r *rig, p model.PersonID) { r.az.Grant(p, "people.profiles.manage") },
	}
	for _, uc := range allUseCases() {
		if !writes[uc.name] {
			continue
		}
		for gname, grant := range grants {
			t.Run(uc.name+"/"+gname, func(t *testing.T) {
				u := newUnit(t)
				me := newPerson()
				grant(u.rig, me)
				wantKind(t, uc.call(asPerson(ctxT(t), me), u.svc), errs.Permission)
				u.noStatements(t, "a refused call")
			})
		}
	}
}

// The superuser passes the permission check (it then reaches the store).
func TestWrites_Superuser_PassesThePermissionCheck(t *testing.T) {
	u := newUnit(t)
	_, err := u.svc.Create(asSuper(ctxT(t), newPerson()), validCreate("Burn 2027"))
	if errs.Is(err, errs.Permission) || errs.Is(err, errs.Unauthenticated) {
		t.Fatalf("superuser refused: %v", err)
	}
}

// ---- validation before the store (F45..F49, F58, F62, F66, F71) -------------------------

func TestCreate_BadName_ReturnsInvalid(t *testing.T) {
	tests := []struct {
		name string
		in   string
		code string
	}{
		{"empty", "", "required"},
		{"101 bytes", repeat("a", events.MaxNameBytes+1), "too_long"},
		{"51 two-byte runes", repeat("é", 51), "too_long"},
		{"control character", "Bu\x07rn", "control_chars"},
		{"newline", "Bu\nrn", "control_chars"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			u := newUnit(t)
			ctx, _ := u.manager()
			in := validCreate("x")
			in.Name = tc.in
			_, err := u.svc.Create(ctx, in)
			wantField(t, err, "name", tc.code)
			if tc.code == "too_long" {
				if got := paramOf(err, "name", "too_long", "max"); got != "100" {
					t.Errorf("max param = %q, want 100", got)
				}
			}
			u.noStatements(t, "an invalid create")
		})
	}
}

func TestCreate_MalformedKind_ReturnsInvalid(t *testing.T) {
	for _, bad := range []string{"Burn", "two words", "-lead", "trail-", "double--dash", "9starts", repeat("a", 33), "x_y", "bürn"} {
		t.Run(bad, func(t *testing.T) {
			u := newUnit(t)
			ctx, _ := u.manager()
			in := validCreate("Burn 2027")
			in.Kind = bad
			_, err := u.svc.Create(ctx, in)
			wantKind(t, err, errs.Invalid)
			if !hasField(err, "kind", "malformed") && !hasField(err, "kind", "too_long") {
				t.Errorf("no kind malformed/too_long field error in %v", err)
			}
			u.noStatements(t, "an invalid create")
		})
	}
	t.Run("empty", func(t *testing.T) {
		u := newUnit(t)
		ctx, _ := u.manager()
		in := validCreate("Burn 2027")
		in.Kind = ""
		_, err := u.svc.Create(ctx, in)
		wantField(t, err, "kind", "")
	})
}

// F47, F48
func TestCreate_BadDates_ReturnsInvalid(t *testing.T) {
	start := epoch.AddDate(0, 0, 30)
	tests := []struct {
		name  string
		dates model.TimeRange
		field string
		code  string
	}{
		{"both zero: start", model.TimeRange{}, "dates.start", "required"},
		{"both zero: end", model.TimeRange{}, "dates.end", "required"},
		{"start zero", model.TimeRange{End: start}, "dates.start", "required"},
		{"end zero", model.TimeRange{Start: start}, "dates.end", "required"},
		{"end equals start", model.TimeRange{Start: start, End: start}, "dates.end", "range"},
		{"end before start", model.TimeRange{Start: start, End: start.Add(-time.Hour)}, "dates.end", "range"},
		{"one nanosecond over the maximum", model.TimeRange{Start: start, End: start.Add(events.MaxEventDuration + time.Nanosecond)}, "dates.end", "range"},
		{"two years", model.TimeRange{Start: start, End: start.AddDate(2, 0, 0)}, "dates.end", "range"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			u := newUnit(t)
			ctx, _ := u.manager()
			in := validCreate("Burn 2027")
			in.Dates = tc.dates
			_, err := u.svc.Create(ctx, in)
			wantField(t, err, tc.field, tc.code)
			u.noStatements(t, "an invalid create")
		})
	}
}

// F49
func TestCreate_BadZone_ReturnsInvalid(t *testing.T) {
	tests := []struct {
		name string
		zone string
		code string
	}{
		{"empty", "", "malformed"},
		{"Local", "Local", "malformed"},
		{"unknown", "Mars/Olympus_Mons", "malformed"},
		{"garbage", "../../etc/passwd", "malformed"},
		{"65 bytes", repeat("a", 65), "too_long"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			u := newUnit(t)
			ctx, _ := u.manager()
			in := validCreate("Burn 2027")
			in.Zone = tc.zone
			_, err := u.svc.Create(ctx, in)
			wantKind(t, err, errs.Invalid)
			if !hasField(err, "zone", tc.code) {
				// An empty zone may be reported as required rather than malformed.
				if tc.zone != "" || !hasField(err, "zone", "required") {
					t.Errorf("no zone/%s field error in %v", tc.code, validateFieldsOf(err))
				}
			}
			u.noStatements(t, "an invalid create")
		})
	}
}

func TestCreate_ValidZones_PassValidation(t *testing.T) {
	for _, zone := range []string{"UTC", "America/Los_Angeles", "Europe/Berlin", "Asia/Kolkata", "Pacific/Chatham"} {
		t.Run(zone, func(t *testing.T) {
			e := events.Event{Name: "Burn", Kind: events.KindBurn, Dates: validDates(), Zone: zone}
			wantNoErr(t, e.Validate())
		})
	}
}

func TestCreate_AllFieldsBad_ReportsEachField(t *testing.T) {
	u := newUnit(t)
	ctx, _ := u.manager()
	_, err := u.svc.Create(ctx, events.CreateInput{Name: "", Kind: "BAD", Dates: model.TimeRange{}, Zone: "Nope/Nope"})
	wantKind(t, err, errs.Invalid)
	for _, f := range []string{"name", "kind", "dates.start", "dates.end", "zone"} {
		if !hasField(err, f, "") {
			t.Errorf("no field error for %s", f)
		}
	}
}

// F58
func TestUpdate_NoFields_ReturnsInvalid(t *testing.T) {
	for name, grant := range map[string]bool{"holder": true, "non-holder": false} {
		t.Run(name, func(t *testing.T) {
			u := newUnit(t)
			me := newPerson()
			if grant {
				u.az.Grant(me, events.PermissionManage)
			}
			_, err := u.svc.Update(asPerson(ctxT(t), me), newEvent(), events.UpdateInput{})
			wantField(t, err, "body", "required")
			u.noStatements(t, "an empty update")
		})
	}
}

func TestUpdate_BadValues_ReturnsInvalid(t *testing.T) {
	start := epoch.AddDate(0, 0, 30)
	tests := []struct {
		name  string
		in    events.UpdateInput
		field string
		code  string
	}{
		{"empty name", events.UpdateInput{Name: str("")}, "name", "required"},
		{"long name", events.UpdateInput{Name: str(repeat("a", 101))}, "name", "too_long"},
		{"control name", events.UpdateInput{Name: str("a\x07b")}, "name", "control_chars"},
		{"malformed kind", events.UpdateInput{Kind: str("Not A Key")}, "kind", "malformed"},
		{"unknown zone", events.UpdateInput{Zone: str("Mars/Olympus_Mons")}, "zone", "malformed"},
		{"empty zone", events.UpdateInput{Zone: str("")}, "zone", ""},
		{"end before start", events.UpdateInput{Dates: &model.TimeRange{Start: start, End: start.Add(-time.Hour)}}, "dates.end", "range"},
		{"zero dates", events.UpdateInput{Dates: &model.TimeRange{}}, "dates.start", "required"},
		{"over a year", events.UpdateInput{Dates: &model.TimeRange{Start: start, End: start.AddDate(2, 0, 0)}}, "dates.end", "range"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			u := newUnit(t)
			ctx, _ := u.manager()
			_, err := u.svc.Update(ctx, newEvent(), tc.in)
			wantField(t, err, tc.field, tc.code)
			u.noStatements(t, "an invalid update")
		})
	}
}

// F62 and the other sides of RunSetup's argument check.
func TestRunSetup_Self_ReturnsInvalid(t *testing.T) {
	u := newUnit(t)
	ctx, _ := u.manager()
	id := newEvent()
	_, err := u.svc.RunSetup(ctx, id, id)
	wantField(t, err, "copy_from", "self")
	u.noStatements(t, "a self copy")
}

func TestRunSetup_ZeroFrom_ReturnsInvalid(t *testing.T) {
	u := newUnit(t)
	ctx, _ := u.manager()
	_, err := u.svc.RunSetup(ctx, newEvent(), model.EventID{})
	wantKind(t, err, errs.Invalid)
	if !hasField(err, "copy_from", "") {
		t.Errorf("no copy_from field error in %v", err)
	}
	u.noStatements(t, "a zero source")
}

// F66
func TestCreateKind_BadInput_ReturnsInvalid(t *testing.T) {
	tests := []struct {
		name  string
		in    events.KindInput
		field string
		code  string
	}{
		{"empty key", events.KindInput{Key: "", Name: "X"}, "key", ""},
		{"upper case key", events.KindInput{Key: "Work", Name: "X"}, "key", "malformed"},
		{"space in key", events.KindInput{Key: "work weekend", Name: "X"}, "key", "malformed"},
		{"leading digit", events.KindInput{Key: "1work", Name: "X"}, "key", "malformed"},
		{"trailing dash", events.KindInput{Key: "work-", Name: "X"}, "key", "malformed"},
		{"33 byte key", events.KindInput{Key: repeat("a", 33), Name: "X"}, "key", "too_long"},
		{"empty name", events.KindInput{Key: "work", Name: ""}, "name", "required"},
		{"61 byte name", events.KindInput{Key: "work", Name: repeat("n", 61)}, "name", "too_long"},
		{"control name", events.KindInput{Key: "work", Name: "a\x07"}, "name", "control_chars"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			u := newUnit(t)
			ctx, _ := u.manager()
			_, err := u.svc.CreateKind(ctx, tc.in)
			wantField(t, err, tc.field, tc.code)
			u.noStatements(t, "an invalid kind")
		})
	}
}

func TestUpdateKind_BadName_ReturnsInvalid(t *testing.T) {
	u := newUnit(t)
	ctx, _ := u.manager()
	_, err := u.svc.UpdateKind(ctx, "burn", events.KindInput{Name: ""})
	wantField(t, err, "name", "required")
	_, err = u.svc.UpdateKind(ctx, "burn", events.KindInput{Name: repeat("n", 61)})
	wantField(t, err, "name", "too_long")
	u.noStatements(t, "an invalid kind rename")
}

// F71
func TestAddScheduleItem_BadInput_ReturnsInvalid(t *testing.T) {
	st := epoch.AddDate(0, 0, 31)
	tests := []struct {
		name  string
		in    events.ScheduleInput
		field string
		code  string
	}{
		{"empty label", events.ScheduleInput{Label: "", Starts: st}, "label", "required"},
		{"101 byte label", events.ScheduleInput{Label: repeat("l", 101), Starts: st}, "label", "too_long"},
		{"control label", events.ScheduleInput{Label: "a\x07", Starts: st}, "label", "control_chars"},
		{"zero start", events.ScheduleInput{Label: "Gate"}, "starts_at", "required"},
		{"end equals start", events.ScheduleInput{Label: "Gate", Starts: st, Ends: st}, "ends_at", "range"},
		{"end before start", events.ScheduleInput{Label: "Gate", Starts: st, Ends: st.Add(-time.Minute)}, "ends_at", "range"},
		{"upper key", events.ScheduleInput{Key: "Temple", Label: "Gate", Starts: st}, "key", "malformed"},
		{"key with space", events.ScheduleInput{Key: "temple burn", Label: "Gate", Starts: st}, "key", "malformed"},
		{"33 byte key", events.ScheduleInput{Key: repeat("a", 33), Label: "Gate", Starts: st}, "key", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			u := newUnit(t)
			ctx, _ := u.manager()
			_, err := u.svc.AddScheduleItem(ctx, newEvent(), tc.in)
			wantField(t, err, tc.field, tc.code)
			_, err = u.svc.UpdateScheduleItem(ctx, newEvent(), model.NewID[events.ScheduleItemID](), tc.in)
			wantField(t, err, tc.field, tc.code)
			u.noStatements(t, "an invalid schedule item")
		})
	}
}

// F76
func TestInfoOf_TooMany_ReturnsInvalid(t *testing.T) {
	u := newUnit(t)
	ids := make([]model.EventID, events.MaxInfo+1)
	for i := range ids {
		ids[i] = newEvent()
	}
	_, err := u.svc.InfoOf(ctxT(t), ids)
	wantField(t, err, "ids", "too_many")
	if got := paramOf(err, "ids", "too_many", "max"); got != "200" {
		t.Errorf("max param = %q, want 200", got)
	}
	if n := len(u.fake.Calls()); n != 0 {
		t.Errorf("%d statements for an over-long list", n)
	}
	if _, err := u.svc.InfoOf(ctxT(t), ids[:events.MaxInfo]); err != nil {
		t.Errorf("exactly MaxInfo IDs: %v", err)
	}
}

func TestInfoOf_EmptyAndNil_ReturnEmpty(t *testing.T) {
	u := newUnit(t)
	for name, ids := range map[string][]model.EventID{"nil": nil, "empty": {}} {
		got, err := u.svc.InfoOf(ctxT(t), ids)
		if err != nil || len(got) != 0 {
			t.Errorf("%s: got %v, %v", name, got, err)
		}
	}
}

func TestList_BadStatus_ReturnsInvalid(t *testing.T) {
	u := newUnit(t)
	_, err := u.svc.List(asPerson(ctxT(t), newPerson()), events.ListQuery{Status: events.Status(99)})
	wantKind(t, err, errs.Invalid)
}

func TestGet_ZeroID_ReturnsNotFound(t *testing.T) {
	u := newUnit(t)
	_, err := u.svc.Get(asPerson(ctxT(t), newPerson()), model.EventID{})
	wantKind(t, err, errs.NotFound)
}

func TestParseStatus_Table(t *testing.T) {
	ok := map[string]events.Status{"any": events.StatusAny, "open": events.StatusOpen, "closed": events.StatusClosed}
	for in, want := range ok {
		got, err := events.ParseStatus(in)
		wantNoErr(t, err)
		if got != want {
			t.Errorf("ParseStatus(%q) = %v, want %v", in, got, want)
		}
	}
	for _, bad := range []string{"ANY", "Open", "all", "opened", "x", " open"} {
		_, err := events.ParseStatus(bad)
		wantField(t, err, "status", "one_of")
	}
}

// ---- cancelled context, read-only copy, store failure ----------------------------------

func TestUseCases_CancelledContext_ReturnsCanceled(t *testing.T) {
	me := newPerson()
	for _, uc := range allUseCases() {
		t.Run(uc.name, func(t *testing.T) {
			r := newRig(t, failDB{err: errs.E("test.failDB", errs.Canceled, context.Canceled)})
			r.link()
			r.az.Grant(me, events.PermissionManage)
			ctx, cancel := context.WithCancel(asPerson(ctxT(t), me))
			cancel()
			err := uc.call(ctx, r.svc)
			if err == nil {
				t.Fatal("a cancelled context succeeded")
			}
			if !errs.Is(err, errs.Canceled) {
				t.Errorf("kind = %v, want Canceled", errs.KindOf(err))
			}
		})
	}
}

func TestWrites_ReadOnlyCopy_ReturnUnavailableReadOnly(t *testing.T) {
	me := newPerson()
	for _, uc := range allUseCases() {
		switch uc.name {
		case "List", "Get", "ListKinds":
			continue
		}
		t.Run(uc.name, func(t *testing.T) {
			u := newUnit(t)
			u.fake.SetReadOnly(true)
			u.az.Grant(me, events.PermissionManage)
			wantReadOnly(t, uc.call(asPerson(ctxT(t), me), u.svc))
			if len(u.rec.Recorded()) != 0 {
				t.Errorf("entries %v on a read-only copy", actionsOf(u.rec.Recorded()))
			}
			if len(u.bus.Published()) != 0 {
				t.Error("a signal was published on a read-only copy")
			}
		})
	}
}

func TestReads_ReadOnlyCopy_Work(t *testing.T) {
	u := newUnit(t)
	u.fake.SetReadOnly(true)
	ctx := asPerson(ctxT(t), newPerson())
	if _, err := u.svc.List(ctx, events.ListQuery{}); err != nil {
		t.Errorf("List: %v", err)
	}
	if _, err := u.svc.ListKinds(ctx); err != nil {
		t.Errorf("ListKinds: %v", err)
	}
	if _, err := u.svc.InfoOf(ctx, []model.EventID{newEvent()}); err != nil {
		t.Errorf("InfoOf: %v", err)
	}
}

func TestStoreFailure_PropagatesKind(t *testing.T) {
	me := newPerson()
	calls := append(allUseCases(), useCase{"InfoOf", func(ctx context.Context, s *events.Service) error {
		_, err := s.InfoOf(ctx, []model.EventID{newEvent()})
		return keep(err)
	}})
	for _, kind := range []errs.Kind{errs.Unavailable, errs.Timeout, errs.Canceled} {
		for _, uc := range calls {
			t.Run(kind.String()+"/"+uc.name, func(t *testing.T) {
				r := newRig(t, failDB{err: errs.E("test.failDB", kind, context.DeadlineExceeded)})
				r.link()
				r.az.Grant(me, events.PermissionManage)
				wantKind(t, uc.call(asPerson(ctxT(t), me), r.svc), kind)
				if len(r.rec.Recorded()) != 0 || len(r.bus.Published()) != 0 {
					t.Error("an audit entry or a signal for a failed call")
				}
			})
		}
	}
}

// ---- permissions, constants, signals ----------------------------------------------------

func TestPermissions_MatchTheSpecTable(t *testing.T) {
	if events.PermissionManage != "events.manage" {
		t.Errorf("PermissionManage = %q", events.PermissionManage)
	}
	if len(events.Permissions) != 1 {
		t.Fatalf("%d declarations, want 1", len(events.Permissions))
	}
	d := events.Permissions[0]
	if d.Name != "events.manage" || d.Scope != authz.ScopeSite || d.Reaches != model.SensitivityInternal {
		t.Errorf("declaration = %+v", d)
	}
	if !slices.Equal(d.StarterRoles, []string{authz.RoleEventAdmin}) {
		t.Errorf("starter roles = %v, want [event-admin]", d.StarterRoles)
	}
	if err := d.Validate(); err != nil {
		t.Errorf("Validate: %v", err)
	}
}

func TestConstantsAndSignals_MatchTheSpec(t *testing.T) {
	if events.MaxNameBytes != 100 || events.MaxKindKeyBytes != 32 || events.MaxKindNameBytes != 60 ||
		events.MaxLabelBytes != 100 || events.MaxZoneBytes != 64 || events.MaxScheduleItems != 100 ||
		events.MaxEventDuration != 366*24*time.Hour || events.MaxInfo != 200 || events.DefaultSetupTimeout != 10*time.Second {
		t.Error("a limit differs from the spec")
	}
	want := map[string]string{
		string(events.SignalCreated):  "events.event-created",
		string(events.SignalChanged):  "events.event-changed",
		string(events.SignalClosed):   "events.event-closed",
		string(events.SignalReopened): "events.event-reopened",
		events.PayloadEvent:           "event",
		events.PayloadCopiedFrom:      "copied-from",
		events.KindBurn:               "burn",
		events.KindDecompression:      "decompression",
		events.KindOther:              "other",
	}
	for got, w := range want {
		if got != w {
			t.Errorf("constant = %q, want %q", got, w)
		}
	}
	if len(events.PublishedSignals) != 4 {
		t.Errorf("PublishedSignals = %v, want the four", events.PublishedSignals)
	}
	if events.Set.Name != "events" || events.Set.Schema != "core" {
		t.Errorf("Set = %q in %q", events.Set.Name, events.Set.Schema)
	}
}

// ---- Validate tables -------------------------------------------------------------------

func TestValidate_Table(t *testing.T) {
	ok := events.Event{ID: newEvent(), Name: "Burn 2027", Kind: events.KindBurn, Dates: validDates(), Zone: validZone, CreatedAt: epoch, UpdatedAt: epoch}
	start := epoch.AddDate(0, 0, 30)
	tests := []struct {
		name  string
		mod   func(*events.Event)
		field string
		code  string
	}{
		{"valid", func(*events.Event) {}, "", ""},
		{"name at the limit", func(e *events.Event) { e.Name = repeat("a", 100) }, "", ""},
		{"exactly the maximum duration", func(e *events.Event) {
			e.Dates = model.TimeRange{Start: start, End: start.Add(events.MaxEventDuration)}
		}, "", ""},
		{"one nanosecond long", func(e *events.Event) { e.Dates = model.TimeRange{Start: start, End: start.Add(time.Nanosecond)} }, "", ""},
		{"UTC", func(e *events.Event) { e.Zone = "UTC" }, "", ""},
		{"empty name", func(e *events.Event) { e.Name = "" }, "name", "required"},
		{"long name", func(e *events.Event) { e.Name = repeat("a", 101) }, "name", "too_long"},
		{"control name", func(e *events.Event) { e.Name = "a\x07" }, "name", "control_chars"},
		{"malformed kind", func(e *events.Event) { e.Kind = "Not A Key" }, "kind", "malformed"},
		{"no start", func(e *events.Event) { e.Dates.Start = time.Time{} }, "dates.start", "required"},
		{"no end", func(e *events.Event) { e.Dates.End = time.Time{} }, "dates.end", "required"},
		{"end not after start", func(e *events.Event) { e.Dates.End = e.Dates.Start }, "dates.end", "range"},
		{"too long", func(e *events.Event) { e.Dates.End = e.Dates.Start.Add(events.MaxEventDuration + time.Nanosecond) }, "dates.end", "range"},
		{"Local zone", func(e *events.Event) { e.Zone = "Local" }, "zone", "malformed"},
		{"unknown zone", func(e *events.Event) { e.Zone = "Nowhere/Land" }, "zone", "malformed"},
		{"long zone", func(e *events.Event) { e.Zone = repeat("z", 65) }, "zone", "too_long"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := ok
			tc.mod(&e)
			err := e.Validate()
			if tc.field == "" {
				wantNoErr(t, err)
				return
			}
			wantField(t, err, tc.field, tc.code)
		})
	}
}

func TestScheduleInputAndKindInput_Validate_Table(t *testing.T) {
	st := epoch.AddDate(0, 0, 31)
	okItem := events.ScheduleInput{Key: "temple-burn", Label: "Temple burn", Starts: st}
	wantNoErr(t, okItem.Validate())
	moment := events.ScheduleInput{Label: "Moment", Starts: st}
	wantNoErr(t, moment.Validate())
	window := events.ScheduleInput{Key: "gate", Label: "Gate", Starts: st, Ends: st.Add(time.Hour)}
	wantNoErr(t, window.Validate())
	bad := events.ScheduleInput{Key: "BAD", Label: "", Starts: time.Time{}}
	err := bad.Validate()
	wantKind(t, err, errs.Invalid)
	for _, f := range []string{"key", "label", "starts_at"} {
		if !hasField(err, f, "") {
			t.Errorf("no field error for %s in %v", f, err)
		}
	}

	wantNoErr(t, events.KindInput{Key: "work-weekend", Name: "Work weekend"}.Validate())
	wantNoErr(t, events.KindInput{Key: repeat("a", 32), Name: repeat("n", 60)}.Validate())
	err = events.KindInput{Key: "", Name: ""}.Validate()
	wantKind(t, err, errs.Invalid)
	for _, f := range []string{"key", "name"} {
		if !hasField(err, f, "") {
			t.Errorf("no field error for %s in %v", f, err)
		}
	}
}

// ---- layering --------------------------------------------------------------------------

func TestImports_NeverModuleApiAppConfigIdentityDepartments(t *testing.T) {
	forbidden := []string{
		"github.com/zedgama3/bms/internal/core/module",
		"github.com/zedgama3/bms/internal/core/identity",
		"github.com/zedgama3/bms/internal/core/departments",
		"github.com/zedgama3/bms/internal/api",
		"github.com/zedgama3/bms/internal/app",
		"github.com/zedgama3/bms/internal/config",
		"github.com/zedgama3/bms/internal/modules",
		"github.com/zedgama3/bms/web",
	}
	for _, dir := range []string{".", "./postgres"} {
		pkg, err := build.ImportDir(dir, 0)
		if err != nil {
			t.Fatalf("ImportDir(%s): %v", dir, err)
		}
		for _, imp := range pkg.Imports {
			for _, bad := range forbidden {
				if imp == bad || strings.HasPrefix(imp, bad+"/") {
					t.Errorf("%s imports %s", dir, imp)
				}
			}
		}
	}
}

// ---- routes, OpenAPI, handlers ----------------------------------------------------------

var specRoutes = []string{
	"GET /", "POST /",
	"GET /kinds", "POST /kinds", "PUT /kinds/{kind}",
	"GET /{event}", "PUT /{event}",
	"POST /{event}/close", "POST /{event}/reopen", "POST /{event}/setup",
	"POST /{event}/schedule", "PUT /{event}/schedule/{item}", "DELETE /{event}/schedule/{item}",
}

func TestRoutes_MatchTheSpecTable(t *testing.T) {
	u := newUnit(t)
	var got []string
	for _, rt := range u.svc.Routes() {
		if rt.Handler == nil {
			t.Errorf("%s %s: nil handler", rt.Method, rt.Pattern)
		}
		got = append(got, rt.Method+" "+rt.Pattern)
	}
	slices.Sort(got)
	want := slices.Clone(specRoutes)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Errorf("routes differ\n got: %v\nwant: %v", got, want)
	}
}

// F80
func TestOpenAPI_NamesExactlyTheRoutes(t *testing.T) {
	u := newUnit(t)
	var doc struct {
		Paths map[string]map[string]json.RawMessage `json:"paths"`
	}
	if err := json.Unmarshal(u.svc.OpenAPI(), &doc); err != nil {
		t.Fatalf("OpenAPI is not JSON: %v", err)
	}
	described := map[string]bool{}
	for path, ops := range doc.Paths {
		for method := range ops {
			described[strings.ToUpper(method)+" "+path] = true
		}
	}
	for _, rt := range specRoutes {
		if !described[rt] {
			t.Errorf("%s is not described", rt)
		}
		delete(described, rt)
	}
	for extra := range described {
		t.Errorf("the fragment describes %s, which has no route", extra)
	}
}

// F42
func TestListHandler_BadStatus_Returns400(t *testing.T) {
	for _, status := range []string{"bogus", "ANY", "all", "1"} {
		t.Run(status, func(t *testing.T) {
			u := newUnit(t)
			w, f := u.do(asPerson(ctxT(t), newPerson()), "GET", "/?status="+status, "")
			if f.count() != 1 {
				t.Fatalf("%d failures (status %d), want 1", f.count(), w.Code)
			}
			wantField(t, f.last(), "status", "one_of")
			if w.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want 400", w.Code)
			}
		})
	}
	for _, status := range []string{"any", "open", "closed"} {
		u := newUnit(t)
		w, f := u.do(asPerson(ctxT(t), newPerson()), "GET", "/?status="+status, "")
		if f.count() != 0 || w.Code != http.StatusOK {
			t.Errorf("status=%s: status %d, failure %v", status, w.Code, f.last())
		}
	}
}

// F79
func TestHandlers_BadInput_Returns400(t *testing.T) {
	item := model.NewID[events.ScheduleItemID]().String()
	ev := newEvent().String()
	tests := []struct {
		name         string
		method, path string
		body         string
		field        string
	}{
		{"get: bad event id", "GET", "/not-a-uuid", "", "event"},
		{"put: bad event id", "PUT", "/not-a-uuid", `{"name":"x"}`, "event"},
		{"close: bad event id", "POST", "/not-a-uuid/close", "", "event"},
		{"reopen: bad event id", "POST", "/12345/reopen", "", "event"},
		{"setup: bad event id", "POST", "/not-a-uuid/setup", `{"copy_from":"` + ev + `"}`, "event"},
		{"schedule add: bad event id", "POST", "/not-a-uuid/schedule", `{"label":"x"}`, "event"},
		{"schedule put: bad event id", "PUT", "/not-a-uuid/schedule/" + item, `{"label":"x"}`, "event"},
		{"schedule delete: bad event id", "DELETE", "/not-a-uuid/schedule/" + item, "", "event"},
		{"schedule put: bad item id", "PUT", "/" + ev + "/schedule/nope", `{"label":"x"}`, "item"},
		{"schedule delete: bad item id", "DELETE", "/" + ev + "/schedule/nope", "", "item"},
		{"create: body not json", "POST", "/", `{nope`, ""},
		{"create: body is an array", "POST", "/", `[]`, ""},
		{"update: body not json", "PUT", "/" + ev, `{nope`, ""},
		{"kind create: body not json", "POST", "/kinds", `{nope`, ""},
		{"kind update: body not json", "PUT", "/kinds/work", `{nope`, ""},
		{"schedule add: body not json", "POST", "/" + ev + "/schedule", `{nope`, ""},
		{"setup: body not json", "POST", "/" + ev + "/setup", `{nope`, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			u := newUnit(t)
			ctx, _ := u.manager()
			w, f := u.do(ctx, tc.method, tc.path, tc.body)
			if f.count() != 1 {
				t.Fatalf("%d failures (status %d), want 1", f.count(), w.Code)
			}
			wantKind(t, f.last(), errs.Invalid)
			if tc.field != "" && !hasField(f.last(), tc.field, "malformed") {
				t.Errorf("no %s/malformed field error in %v", tc.field, validateFieldsOf(f.last()))
			}
			if w.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want 400", w.Code)
			}
		})
	}
}

func TestHandlers_Errors_MapToStatus(t *testing.T) {
	me := newPerson()
	ev := newEvent().String()
	item := model.NewID[events.ScheduleItemID]().String()
	body := `{"name":"Burn 2027","kind":"burn","dates":{"start":"2027-08-24T00:00:00Z","end":"2027-09-01T00:00:00Z"},"zone":"America/Los_Angeles"}`
	tests := []struct {
		name         string
		method, path string
		body         string
		signed       bool
		grant        bool
		kind         errs.Kind
		status       int
	}{
		{"list without a session", "GET", "/", "", false, false, errs.Unauthenticated, 401},
		{"kinds without a session", "GET", "/kinds", "", false, false, errs.Unauthenticated, 401},
		{"get without a session", "GET", "/" + ev, "", false, false, errs.Unauthenticated, 401},
		{"create without a session", "POST", "/", body, false, false, errs.Unauthenticated, 401},
		{"create without permission", "POST", "/", body, true, false, errs.Permission, 403},
		{"update without permission", "PUT", "/" + ev, `{"name":"x"}`, true, false, errs.Permission, 403},
		{"close without permission", "POST", "/" + ev + "/close", "", true, false, errs.Permission, 403},
		{"reopen without permission", "POST", "/" + ev + "/reopen", "", true, false, errs.Permission, 403},
		{"kind create without permission", "POST", "/kinds", `{"key":"work","name":"Work"}`, true, false, errs.Permission, 403},
		{"kind update without permission", "PUT", "/kinds/burn", `{"name":"Burn"}`, true, false, errs.Permission, 403},
		{"schedule add without permission", "POST", "/" + ev + "/schedule", `{"label":"Gate","starts_at":"2027-08-25T00:00:00Z"}`, true, false, errs.Permission, 403},
		{"schedule delete without permission", "DELETE", "/" + ev + "/schedule/" + item, "", true, false, errs.Permission, 403},
		{"create with a bad name", "POST", "/", strings.Replace(body, "Burn 2027", "", 1), true, true, errs.Invalid, 400},
		{"update with no field", "PUT", "/" + ev, `{}`, true, true, errs.Invalid, 400},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			u := newUnit(t)
			ctx := ctxT(t)
			if tc.signed {
				ctx = asPerson(ctx, me)
			}
			if tc.grant {
				u.az.Grant(me, events.PermissionManage)
			}
			w, f := u.do(ctx, tc.method, tc.path, tc.body)
			if f.count() != 1 {
				t.Fatalf("%d failures handed to httpx.Fail (status %d), want 1", f.count(), w.Code)
			}
			wantKind(t, f.last(), tc.kind)
			if w.Code != tc.status {
				t.Errorf("status = %d, want %d", w.Code, tc.status)
			}
		})
	}
}

func TestHandlers_WriteOnReadOnlyCopy_Maps503(t *testing.T) {
	u := newUnit(t)
	u.fake.SetReadOnly(true)
	me := newPerson()
	u.az.Grant(me, events.PermissionManage)
	w, f := u.do(asPerson(ctxT(t), me), "POST", "/kinds", `{"key":"work","name":"Work"}`)
	if f.count() != 1 {
		t.Fatalf("%d failures, status %d", f.count(), w.Code)
	}
	wantReadOnly(t, f.last())
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", w.Code)
	}
}

// The static kinds route must not be swallowed by /{event}.
func TestHandlers_KindsRoute_IsNotAnEventID(t *testing.T) {
	u := newUnit(t)
	w, f := u.do(asPerson(ctxT(t), newPerson()), "GET", "/kinds", "")
	if f.count() != 0 || w.Code != http.StatusOK {
		t.Fatalf("GET /kinds: status %d, failure %v", w.Code, f.last())
	}
	if !json.Valid(w.Body.Bytes()) {
		t.Errorf("body is not JSON: %s", w.Body.String())
	}
}

// TestWire_Shapes pins the JSON the web screens read.
func TestWire_Shapes(t *testing.T) {
	keys := func(t *testing.T, v any) map[string]json.RawMessage {
		t.Helper()
		raw, err := json.Marshal(v)
		wantNoErr(t, err)
		var m map[string]json.RawMessage
		wantNoErr(t, json.Unmarshal(raw, &m))
		return m
	}
	equal := func(t *testing.T, got map[string]json.RawMessage, want ...string) {
		t.Helper()
		if len(got) != len(want) {
			t.Errorf("keys = %v, want %v", got, want)
		}
		for _, k := range want {
			if _, ok := got[k]; !ok {
				t.Errorf("missing key %q", k)
			}
		}
	}
	ev := events.Event{ID: newEvent(), Name: "Burn", Kind: "burn", Dates: validDates(), Zone: validZone, CreatedAt: epoch, UpdatedAt: epoch}
	equal(t, keys(t, ev), "id", "name", "kind", "dates", "zone", "closed", "created_at", "updated_at")
	ev.Closed, ev.ClosedAt = true, epoch
	m := keys(t, ev)
	equal(t, m, "id", "name", "kind", "dates", "zone", "closed", "closed_at", "created_at", "updated_at")
	equal(t, func() map[string]json.RawMessage {
		var d map[string]json.RawMessage
		wantNoErr(t, json.Unmarshal(m["dates"], &d))
		return d
	}(), "start", "end")

	equal(t, keys(t, events.Kind{Key: "burn", Name: "Burn", Builtin: true, CreatedAt: epoch, UpdatedAt: epoch}),
		"key", "name", "builtin", "created_at", "updated_at")

	item := events.ScheduleItem{ID: model.NewID[events.ScheduleItemID](), Event: ev.ID, Label: "Gate", Starts: epoch}
	equal(t, keys(t, item), "id", "event", "label", "starts_at", "position")
	item.Key, item.Ends = "gate", epoch.Add(time.Hour)
	equal(t, keys(t, item), "id", "event", "key", "label", "starts_at", "ends_at", "position")

	det := keys(t, events.Detail{Event: ev, Schedule: []events.ScheduleItem{item}})
	equal(t, det, "id", "name", "kind", "dates", "zone", "closed", "closed_at", "created_at", "updated_at", "schedule")

	cr := keys(t, events.Created{Event: ev, Setup: []events.SetupResult{{Module: "volunteers", Items: 3}}})
	equal(t, cr, "event", "setup")
	var res []map[string]json.RawMessage
	wantNoErr(t, json.Unmarshal(cr["setup"], &res))
	equal(t, res[0], "module", "items", "failed")
	failed := keys(t, events.SetupResult{Module: "incidents", Failed: true, Code: "permission"})
	equal(t, failed, "module", "items", "failed", "code")

	var in events.CreateInput
	wantNoErr(t, json.Unmarshal([]byte(`{"name":"A","kind":"burn","zone":"UTC","dates":{"start":"2027-08-24T00:00:00Z","end":"2027-09-01T00:00:00Z"}}`), &in))
	if in.Name != "A" || in.Kind != "burn" || in.Zone != "UTC" || in.Dates.Start.IsZero() || !in.CopyFrom.IsZero() {
		t.Errorf("CreateInput decoded as %+v", in)
	}
	var up events.UpdateInput
	wantNoErr(t, json.Unmarshal([]byte(`{"zone":"UTC"}`), &up))
	if up.Name != nil || up.Kind != nil || up.Dates != nil || up.Zone == nil || *up.Zone != "UTC" {
		t.Errorf("UpdateInput decoded as %+v", up)
	}
	var si events.ScheduleInput
	wantNoErr(t, json.Unmarshal([]byte(`{"key":"gate","label":"Gate","starts_at":"2027-08-25T00:00:00Z"}`), &si))
	if si.Key != "gate" || si.Label != "Gate" || si.Starts.IsZero() || !si.Ends.IsZero() {
		t.Errorf("ScheduleInput decoded as %+v", si)
	}
}
