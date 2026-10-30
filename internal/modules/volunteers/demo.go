package volunteers

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"time"

	"github.com/zedgama3/bms/internal/contracts/demoseed"
	"github.com/zedgama3/bms/internal/core/files"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/validate"
	"github.com/zedgama3/bms/internal/model"
	"github.com/zedgama3/bms/internal/modules/volunteers/vol"
)

// demoSeeder is the module's demoseed.Provider (module-volunteers.md, Amendment 2026-10-28,
// D53). It holds the service and nothing else, so Seed is not a method of Service.
type demoSeeder struct{ s *Service }

// The demo's fixed words. Everything here is invented; nothing is about a real person.
const (
	demoRangerTraining = "Ranger training"
	demoICS100         = "ICS-100"
	demoOver18         = "Over 18"
	demoFirstAid       = "First aid"

	demoDirtRanger = "Dirt Ranger"
	demoKhaki      = "Khaki"
	demoGateCrew   = "Gate Crew"
	demoGreeter    = "Greeter"
	demoMedic      = "Medic"
	demoInventory  = "Inventory Crew"
)

var errDemoRequest = errors.New("the demo request is incomplete")

// demoRun is one Seed call: the service, the request, the contexts of the three actors and
// the running count of created records.
type demoRun struct {
	s   *Service
	req demoseed.Request

	steward, lead, participant context.Context

	loc   *time.Location
	zone  string
	first time.Time // midnight of the event's first day (its Thursday) in loc
	ends  time.Time // the event's end
	today vol.Date

	quals     map[string]vol.QualificationID
	positions map[string]vol.PositionID
	shiftIDs  map[demoShiftKey]vol.ShiftID
	items     int
}

// demoPositions lists the positions in creation order. Requirements name catalogue entries.
var demoPositions = []struct {
	name, desc, dept string
	access           vol.Access
	requires         []string
}{
	{demoDirtRanger, "Patrol on foot or by bike, always in a pair.", demoseed.DeptRangers, vol.AccessOpen, []string{demoRangerTraining, demoOver18}},
	{demoKhaki, "Runs a Ranger shift: briefs, pairs and dispatches, keeps the shift log.", demoseed.DeptRangers, vol.AccessMembers, []string{demoRangerTraining, demoICS100, demoOver18}},
	{demoGateCrew, "Check tickets, waivers and wristbands at the entrance.", demoseed.DeptGate, vol.AccessOpen, nil},
	{demoGreeter, "Welcome arrivals and point them to camp.", demoseed.DeptGreeters, vol.AccessOpen, nil},
	{demoMedic, "Staff the medical tent.", demoseed.DeptMedical, vol.AccessMembers, []string{demoFirstAid}},
	{demoInventory, "Count and shelve the department's gear.", demoseed.DeptDPW, vol.AccessOpen, nil},
}

// demoShiftRow is one shift pattern; it is created on each of the event's four days. A
// shift whose end hour is not after its start hour ends the next morning.
type demoShiftRow struct {
	position, part string
	from, to       int
	slots          int
	location       string
}

var demoShiftRows = []demoShiftRow{
	{demoDirtRanger, "morning", 8, 14, 4, "Ranger HQ"},
	{demoDirtRanger, "afternoon", 14, 20, 4, "Ranger HQ"},
	{demoDirtRanger, "night", 20, 2, 4, "Ranger HQ"},
	{demoKhaki, "day", 7, 15, 1, "Ranger HQ"},
	{demoKhaki, "evening", 15, 23, 1, "Ranger HQ"},
	{demoGateCrew, "midday", 10, 14, 3, "Main gate"},
	{demoGateCrew, "afternoon", 14, 18, 3, "Main gate"},
	{demoGateCrew, "evening", 18, 22, 3, "Main gate"},
	{demoGreeter, "midday", 10, 14, 3, "Greeter tent"},
	{demoGreeter, "afternoon", 14, 18, 3, "Greeter tent"},
	{demoMedic, "day", 8, 20, 2, "Medical tent"},
	{demoMedic, "night", 20, 8, 2, "Medical tent"},
}

// demoDays is how many days (Thursday to Sunday) the event's shifts cover.
const demoDays = 4

// demoShiftKey names one created shift: the position, the pattern's index among that
// position's rows and the day (0 = Thursday).
type demoShiftKey struct {
	position string
	part     int
	day      int
}

// Seed creates the volunteers demo data described in module-volunteers.md ("Amendment
// 2026-10-28"), in order: qualifications, positions with their requirements, members,
// records, shifts, then sign-ups, acting only through the module's own use cases as the
// demo accounts, so every permission, audit and validation rule applies. Result.Items
// counts the records created. It fails with the kind of the first step that failed,
// wrapped with the step's name: errs.Permission when an actor lacks a permission,
// errs.Invalid when a row does not validate, errs.Conflict (unnamed) when a booked person
// has no playa name, errs.Invalid when the request lacks an event, department or person, and
// the kinds of Actors.As and of the services. It stops at the first failure and does not
// undo what it created (the demo reset does).
func (d demoSeeder) Seed(ctx context.Context, req demoseed.Request) (demoseed.Result, error) {
	const op = "volunteers.demoSeed"
	r := &demoRun{
		s: d.s, req: req, quals: map[string]vol.QualificationID{}, positions: map[string]vol.PositionID{},
		shiftIDs: map[demoShiftKey]vol.ShiftID{},
	}
	if err := r.begin(ctx); err != nil {
		return demoseed.Result{}, fmt.Errorf("%s: %w", op, err)
	}
	for _, step := range []struct {
		name string
		do   func() error
	}{
		{"qualifications", r.qualifications},
		{"positions", r.createPositions},
		{"members", r.members},
		{"records", r.records},
		{"shifts", r.shifts},
		{"sign-ups", r.signups},
	} {
		if err := ctx.Err(); err != nil {
			return demoseed.Result{}, errs.E(op+": "+step.name, errs.KindOf(err), err)
		}
		if err := step.do(); err != nil {
			return demoseed.Result{}, fmt.Errorf("%s: %s: %w", op, step.name, err)
		}
	}
	return demoseed.Result{Items: r.items}, nil
}

// begin checks the request, signs in the three actors and reads the event.
func (r *demoRun) begin(ctx context.Context) error {
	if r.req.Actors == nil {
		return errs.E("volunteers.demoSeed.begin", errs.Invalid, fmt.Errorf("%w: no actors", errDemoRequest))
	}
	if r.req.Event.IsZero() {
		return errs.E("volunteers.demoSeed.begin", errs.Invalid, fmt.Errorf("%w: no event", errDemoRequest))
	}
	for _, k := range []string{demoseed.DeptRangers, demoseed.DeptGate, demoseed.DeptGreeters, demoseed.DeptMedical, demoseed.DeptDPW} {
		if r.req.Departments[k].IsZero() {
			return errs.E("volunteers.demoSeed.begin", errs.Invalid, fmt.Errorf("%w: no department %q", errDemoRequest, k))
		}
	}
	for _, k := range []string{demoseed.ActorParticipant, demoseed.ActorLead} {
		if r.req.People[k].IsZero() {
			return errs.E("volunteers.demoSeed.begin", errs.Invalid, fmt.Errorf("%w: no person %q", errDemoRequest, k))
		}
	}
	for n := 1; n <= demoseed.WalkOns; n++ {
		if r.walkOn(n).IsZero() {
			return errs.E("volunteers.demoSeed.begin", errs.Invalid, fmt.Errorf("%w: no person %q", errDemoRequest, demoseed.WalkOnKey(n)))
		}
	}
	var err error
	for _, a := range []struct {
		key string
		dst *context.Context
	}{
		{demoseed.ActorSteward, &r.steward}, {demoseed.ActorLead, &r.lead}, {demoseed.ActorParticipant, &r.participant},
	} {
		if *a.dst, err = r.req.Actors.As(ctx, a.key); err != nil {
			return fmt.Errorf("sign in as %s: %w", a.key, err)
		}
	}
	infos, err := r.s.events.InfoOf(ctx, []model.EventID{r.req.Event})
	if err != nil {
		return fmt.Errorf("event: %w", err)
	}
	info, ok := infos[r.req.Event]
	if !ok {
		return errs.E("volunteers.demoSeed.begin", errs.NotFound, fmt.Errorf("%w: the event is unknown", errDemoRequest))
	}
	if r.loc, err = time.LoadLocation(info.Zone); err != nil {
		return errs.E("volunteers.demoSeed.begin", errs.Invalid, fmt.Errorf("event zone %q: %w", info.Zone, err))
	}
	start, end := info.Dates.Start, info.Dates.End
	if start.IsZero() || end.IsZero() {
		return errs.E("volunteers.demoSeed.begin", errs.Invalid, &validate.FieldError{
			Field: "event", Code: validate.CodeRequired, Reason: "the event has no dates",
		})
	}
	r.zone, r.ends = info.Zone, end
	y, m, day := start.In(r.loc).Date()
	r.first = time.Date(y, m, day, 0, 0, 0, 0, r.loc)
	r.today = r.s.today()
	return nil
}

// walkOn returns walk-on n's person.
func (r *demoRun) walkOn(n int) model.PersonID { return r.req.People[demoseed.WalkOnKey(n)] }

// dept returns the department of a Dept* key.
func (r *demoRun) dept(key string) model.DepartmentID { return r.req.Departments[key] }

// qualifications creates the catalogue: the org-wide entries and First aid as the steward
// (qualifications.manage site-wide), Ranger training as the lead.
func (r *demoRun) qualifications() error {
	for _, q := range []struct {
		as   context.Context
		in   vol.QualificationInput
		desc string
	}{
		{r.steward, vol.QualificationInput{Name: demoICS100, Description: "Incident Command System, introduction. A copy of the certificate is enough."}, ""},
		{r.steward, vol.QualificationInput{Name: demoOver18, Description: "The volunteer is an adult."}, ""},
		{r.lead, vol.QualificationInput{Department: r.dept(demoseed.DeptRangers), Name: demoRangerTraining, Description: "The Rangers' field training, renewed every year.", ValidityDays: 365}, ""},
		{r.steward, vol.QualificationInput{Department: r.dept(demoseed.DeptMedical), Name: demoFirstAid, Description: "A current first aid course.", ValidityDays: 730}, ""},
	} {
		created, err := r.s.CreateQualification(q.as, q.in)
		if err != nil {
			return fmt.Errorf("qualification %q: %w", q.in.Name, err)
		}
		r.quals[q.in.Name] = created.ID
		r.items++
	}
	return nil
}

// createPositions creates the positions as the lead and sets each one's requirements.
func (r *demoRun) createPositions() error {
	for _, p := range demoPositions {
		created, err := r.s.CreatePosition(r.lead, r.dept(p.dept), vol.PositionInput{Name: p.name, Description: p.desc, Access: p.access})
		if err != nil {
			return fmt.Errorf("position %q: %w", p.name, err)
		}
		r.positions[p.name] = created.ID
		r.items++
		if len(p.requires) == 0 {
			continue
		}
		ids := make([]vol.QualificationID, 0, len(p.requires))
		for _, name := range p.requires {
			ids = append(ids, r.quals[name])
		}
		if _, err := r.s.SetRequirements(r.lead, created.ID, ids); err != nil {
			return fmt.Errorf("requirements of %q: %w", p.name, err)
		}
	}
	return nil
}

// demoRangerWalkOns and demoMedicalWalkOns number the walk-ons who are members. Walk-on 07 is
// deliberately neither: the lead books them with a waiver.
var (
	demoRangerWalkOns  = []int{1, 2, 3, 4, 5, 6}
	demoMedicalWalkOns = []int{8, 9, 10, 11}
)

// members adds the participant, the lead and six walk-ons to Rangers and four walk-ons to
// Medical, as the lead.
func (r *demoRun) members() error {
	rangers := []model.PersonID{r.req.People[demoseed.ActorParticipant], r.req.People[demoseed.ActorLead]}
	for _, n := range demoRangerWalkOns {
		rangers = append(rangers, r.walkOn(n))
	}
	medical := make([]model.PersonID, 0, len(demoMedicalWalkOns))
	for _, n := range demoMedicalWalkOns {
		medical = append(medical, r.walkOn(n))
	}
	for _, g := range []struct {
		dept   model.DepartmentID
		people []model.PersonID
	}{{r.dept(demoseed.DeptRangers), rangers}, {r.dept(demoseed.DeptMedical), medical}} {
		for _, p := range g.people {
			if _, err := r.s.AddMember(r.lead, g.dept, p); err != nil {
				return fmt.Errorf("member: %w", err)
			}
			r.items++
		}
	}
	return nil
}

// records verifies Ranger training and Over 18 for the participant and the Rangers walk-ons,
// as the lead, and files the participant's pending self-upload of an ICS-100 certificate.
// The lead's own records are not created: a lead may not verify their own (Invalid self) and
// no other demo account may verify at Rangers.
func (r *demoRun) records() error {
	people := []model.PersonID{r.req.People[demoseed.ActorParticipant]}
	for _, n := range demoRangerWalkOns {
		people = append(people, r.walkOn(n))
	}
	for _, p := range people {
		for _, rec := range []struct {
			qual string
			ago  int
		}{{demoRangerTraining, 60}, {demoOver18, 30}} {
			if _, err := r.s.RecordQualification(r.lead, vol.RecordInput{
				Qualification: r.quals[rec.qual], Person: p, AcquiredOn: r.today.AddDays(-rec.ago),
			}); err != nil {
				return fmt.Errorf("record %q: %w", rec.qual, err)
			}
			r.items++
		}
	}
	body, err := demoCertificate()
	if err != nil {
		return err
	}
	if _, err := r.s.UploadCertificate(r.participant, vol.UploadInput{
		Qualification: r.quals[demoICS100], AcquiredOn: r.today.AddDays(-10),
		Upload: &files.Multipart{Name: "ics-100-certificate.png", DeclaredType: "image/png", Body: bytes.NewReader(body)},
	}); err != nil {
		return fmt.Errorf("upload certificate: %w", err)
	}
	r.items++
	return nil
}

// demoCertificate returns a generated 1x1 PNG, the stand-in for a scanned certificate.
func demoCertificate() ([]byte, error) {
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	img.Set(0, 0, color.RGBA{R: 0xd9, G: 0x6c, B: 0x1e, A: 0xff})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, errs.E("volunteers.demoCertificate", errs.Internal, err)
	}
	return buf.Bytes(), nil
}

// at returns the instant on event day day (0 = Thursday) at hour h of local time.
func (r *demoRun) at(day, h int) time.Time {
	y, m, d := r.first.Date()
	return time.Date(y, m, d+day, h, 0, 0, 0, r.loc)
}

// shifts creates the event's shifts, Thursday to Sunday, and the standing inventory shift
// two weeks after the event, as the lead.
func (r *demoRun) shifts() error {
	rowsOf := map[string]int{} // position -> rows seen, the part index
	for _, row := range demoShiftRows {
		part := rowsOf[row.position]
		rowsOf[row.position]++
		pos := r.positions[row.position]
		dept := r.positionDept(row.position)
		for day := range demoDays {
			end := row.to
			if row.to <= row.from {
				end += 24
			}
			label := r.at(day, 12).Weekday().String() + " " + row.part
			sh, err := r.s.CreateShift(r.lead, dept, vol.ShiftInput{
				Position: pos, Event: r.req.Event, Label: label,
				Starts: r.at(day, row.from), Ends: r.at(day, end), Slots: row.slots, Location: row.location,
			})
			if err != nil {
				return fmt.Errorf("shift %q %q: %w", row.position, label, err)
			}
			r.shiftIDs[demoShiftKey{row.position, part, day}] = sh.ID
			r.items++
		}
	}
	endDay := time.Date(r.ends.In(r.loc).Year(), r.ends.In(r.loc).Month(), r.ends.In(r.loc).Day()+14, 0, 0, 0, 0, r.loc)
	_, err := r.s.CreateShift(r.lead, r.dept(demoseed.DeptDPW), vol.ShiftInput{
		Position: r.positions[demoInventory], Label: "Winter inventory",
		Starts: endDay.Add(9 * time.Hour), Ends: endDay.Add(13 * time.Hour), Zone: r.zone, Slots: 6, Location: "Warehouse",
		Notes: "Count the gear and shelve it for next year.",
	})
	if err != nil {
		return fmt.Errorf("shift %q: %w", "Winter inventory", err)
	}
	r.items++
	return nil
}

// positionDept returns the department of a demo position.
func (r *demoRun) positionDept(name string) model.DepartmentID {
	for _, p := range demoPositions {
		if p.name == name {
			return r.dept(p.dept)
		}
	}
	return model.DepartmentID{}
}

// shift returns a created shift's ID.
func (r *demoRun) shift(position string, part, day int) vol.ShiftID {
	return r.shiftIDs[demoShiftKey{position, part, day}]
}

// Parts and days the sign-ups refer to (indexes into demoShiftRows, per position).
const (
	demoThursday, demoFriday, demoSaturday, demoSunday = 0, 1, 2, 3

	demoDirtMorning, demoDirtAfternoon, demoDirtNight = 0, 1, 2
	demoKhakiDay                                      = 0
)

// signups books the participant's two Dirt Ranger shifts, then, as the lead, assigns the
// walk-ons (one full Dirt Ranger shift, one booking on each Gate Crew and Greeter shift),
// holds Khaki on Friday, books walk-on 07 with a waiver and cancels the Sunday night patrol.
// Every person booked has a playa name, which the service requires.
func (r *demoRun) signups() error {
	for _, w := range []struct{ part, day int }{{demoDirtAfternoon, demoThursday}, {demoDirtMorning, demoSaturday}} {
		if _, err := r.s.SignUp(r.participant, r.shift(demoDirtRanger, w.part, w.day)); err != nil {
			return fmt.Errorf("sign up for %s: %w", demoDirtRanger, err)
		}
		r.items++
	}
	assign := func(position string, part, day int, person model.PersonID, waive bool) error {
		if _, err := r.s.Assign(r.lead, r.shift(position, part, day), vol.AssignInput{Person: person, Waive: waive}); err != nil {
			return fmt.Errorf("assign to %s: %w", position, err)
		}
		r.items++
		return nil
	}
	for _, n := range []int{1, 2, 3, 4} { // Friday morning is full: four slots, four Rangers
		if err := assign(demoDirtRanger, demoDirtMorning, demoFriday, r.walkOn(n), false); err != nil {
			return err
		}
	}
	for day := range demoDays {
		for part, n := range []int{9, 10, 11} {
			if err := assign(demoGateCrew, part, day, r.walkOn(n), false); err != nil {
				return err
			}
		}
		for part, n := range []int{12, 8} {
			if err := assign(demoGreeter, part, day, r.walkOn(n), false); err != nil {
				return err
			}
		}
	}
	// The lead holds Khaki on Friday. The lead has no records of their own (see records), so
	// the explicit, recorded waiver stands in for them.
	if err := assign(demoKhaki, demoKhakiDay, demoFriday, r.req.People[demoseed.ActorLead], true); err != nil {
		return err
	}
	// Walk-on 07 has no Ranger training: the waiver the roster shows.
	if err := assign(demoDirtRanger, demoDirtAfternoon, demoSaturday, r.walkOn(7), true); err != nil {
		return err
	}
	if err := r.s.CancelShift(r.lead, r.shift(demoDirtRanger, demoDirtNight, demoSunday)); err != nil {
		return fmt.Errorf("cancel the Sunday %s shift: %w", demoDirtRanger, err)
	}
	return nil
}
