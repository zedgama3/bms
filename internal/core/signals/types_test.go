package signals_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/core/signals"
	"github.com/zedgama3/bms/internal/core/telemetry"
	"github.com/zedgama3/bms/internal/core/telemetry/telemetrytest"
	"github.com/zedgama3/bms/internal/lib/clock"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/validate"
	"github.com/zedgama3/bms/internal/model"
)

func fieldNames(err error) map[string]bool {
	out := map[string]bool{}
	for _, fe := range validate.Fields(err) {
		out[fe.Field] = true
	}
	return out
}

func TestNameValidate_AcceptsExamples(t *testing.T) {
	for _, n := range []signals.Name{"hello.greeted", "volunteers.shift-completed", "people.person-created", "a1.b-2-c"} {
		if err := n.Validate(); err != nil {
			t.Errorf("%q: %v", n, err)
		}
	}
	if got := signals.Name("volunteers.shift-completed").Publisher(); got != "volunteers" {
		t.Errorf("Publisher = %q", got)
	}
}

func TestNameValidate_Table(t *testing.T) {
	seg64 := "a" + strings.Repeat("b", 63)
	seg65 := seg64 + "c"
	tests := []struct {
		name string
		in   signals.Name
		ok   bool
	}{
		{"empty", "", false},
		{"one segment", "hello", false},
		{"three segments", "a.b.c", false},
		{"upper publisher", "Hello.greeted", false},
		{"upper ident", "hello.Greeted", false},
		{"underscore", "hello.some_thing", false},
		{"empty second", "hello.", false},
		{"empty first", ".greeted", false},
		{"leading digit", "1a.greeted", false},
		{"trailing hyphen", "hello.greeted-", false},
		{"double hyphen", "hello.a--b", false},
		{"space", "hello .greeted", false},
		{"64 byte segments", signals.Name(seg64 + "." + seg64), true},
		{"65 byte first", signals.Name(seg65 + ".x"), false},
		{"65 byte second", signals.Name("x." + seg65), false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.in.Validate()
			if tc.ok {
				noErr(t, err)
				return
			}
			wantKind(t, err, errs.Invalid)
		})
	}
}

func TestPayloadValidate_NilAndEmptyAreValid(t *testing.T) {
	noErr(t, signals.Payload(nil).Validate())
	noErr(t, signals.NewPayload().Validate())
}

func TestPayloadValidate_BadKey_ReturnsInvalid(t *testing.T) {
	id := model.NewID[model.PersonID]()
	for _, key := range []string{"", "Person", "a_b", "-a", "a b", strings.Repeat("a", 65)} {
		p := signals.NewPayload(signals.Ref(key, id))
		err := p.Validate()
		wantKind(t, err, errs.Invalid)
		if len(validate.Fields(err)) == 0 {
			t.Errorf("key %q: no FieldError", key)
		}
	}
	// the error names the field "payload.<key>"
	err := signals.NewPayload(signals.Ref("Bad", id)).Validate()
	if !fieldNames(err)["payload.Bad"] {
		t.Errorf("fields = %v, want payload.Bad", fieldNames(err))
	}
}

func TestPayloadValidate_ZeroID_ReturnsInvalid(t *testing.T) {
	p := signals.NewPayload(signals.Ref("person", model.PersonID{}), signals.Ref("shift", model.NewID[model.EventID]()))
	err := p.Validate()
	wantKind(t, err, errs.Invalid)
	f := fieldNames(err)
	if !f["payload.person"] || f["payload.shift"] {
		t.Errorf("fields = %v, want exactly payload.person", f)
	}
}

func TestPayloadValidate_TooManyOrRepeated_ReturnsInvalid(t *testing.T) {
	build := func(n int) signals.Payload {
		var es []signals.Entry
		for i := 0; i < n; i++ {
			es = append(es, signals.Ref("k"+string(rune('a'+i)), model.NewID[model.PersonID]()))
		}
		return signals.NewPayload(es...)
	}
	noErr(t, build(signals.MaxRefs).Validate())
	err := build(signals.MaxRefs + 1).Validate()
	wantKind(t, err, errs.Invalid)
	if !fieldNames(err)["payload"] {
		t.Errorf("fields = %v, want payload", fieldNames(err))
	}
	rep := signals.NewPayload(
		signals.Ref("person", model.NewID[model.PersonID]()),
		signals.Ref("person", model.NewID[model.PersonID]()),
	)
	err = rep.Validate()
	wantKind(t, err, errs.Invalid)
	if !fieldNames(err)["payload"] {
		t.Errorf("repeated key: fields = %v, want payload", fieldNames(err))
	}
}

func TestGet_MissingKey_ReturnsInvalid(t *testing.T) {
	p := signals.Payload{"zero": signals.RecordID{}}
	for _, key := range []string{"absent", "zero"} {
		_, err := signals.Get[model.PersonID](p, key)
		wantKind(t, err, errs.Invalid)
		if !strings.Contains(err.Error(), key) {
			t.Errorf("error does not name key %q: %v", key, err)
		}
	}
	_, err := signals.Get[model.PersonID](nil, "person")
	wantKind(t, err, errs.Invalid)
}

func TestRef_TypedIDRoundTrip(t *testing.T) {
	pid := model.NewID[model.PersonID]()
	eid := model.NewID[model.EventID]()
	p := signals.NewPayload(signals.Ref("person", pid), signals.Ref("event", eid))
	noErr(t, p.Validate())
	gp, err := signals.Get[model.PersonID](p, "person")
	noErr(t, err)
	if gp != pid {
		t.Errorf("person = %v, want %v", gp, pid)
	}
	ge, err := signals.Get[model.EventID](p, "event")
	noErr(t, err)
	if ge != eid {
		t.Errorf("event = %v, want %v", ge, eid)
	}
}

func TestPayload_JSONRoundTrip(t *testing.T) {
	pid := model.NewID[model.PersonID]()
	p := signals.NewPayload(signals.Ref("person", pid))
	b, err := json.Marshal(p)
	noErr(t, err)
	if !strings.Contains(string(b), pid.String()) {
		t.Errorf("json %s lacks canonical %s", b, pid.String())
	}
	var back signals.Payload
	noErr(t, json.Unmarshal(b, &back))
	noErr(t, back.Validate())
	got, err := signals.Get[model.PersonID](back, "person")
	noErr(t, err)
	if got != pid {
		t.Errorf("after decode = %v, want %v", got, pid)
	}
	// a whole Signal round-trips too: it is serializable
	s := validSignal(nameX)
	b, err = json.Marshal(s)
	noErr(t, err)
	var sb signals.Signal
	noErr(t, json.Unmarshal(b, &sb))
	noErr(t, sb.Validate())
	if sb.ID != s.ID || sb.Name != s.Name || !sb.At.Equal(s.At) {
		t.Errorf("signal round trip changed: %+v vs %+v", sb, s)
	}
}

func TestNewPayload_CopiesSoCallerMutationIsHarmless(t *testing.T) {
	id := model.NewID[model.PersonID]()
	entries := []signals.Entry{signals.Ref("person", id)}
	p := signals.NewPayload(entries...)
	entries[0] = signals.Ref("other", model.NewID[model.PersonID]())
	got, err := signals.Get[model.PersonID](p, "person")
	noErr(t, err)
	if got != id {
		t.Errorf("payload followed the caller's slice")
	}
}

func TestSignalValidate_Table(t *testing.T) {
	good := validSignal(nameX)
	noErr(t, good.Validate())

	mut := func(f func(*signals.Signal)) signals.Signal {
		s := validSignal(nameX)
		f(&s)
		return s
	}
	tests := []struct {
		name string
		s    signals.Signal
		want int // number of FieldErrors
	}{
		{"zero ID", mut(func(s *signals.Signal) { s.ID = signals.SignalID{} }), 1},
		{"bad name", mut(func(s *signals.Signal) { s.Name = "nope" }), 1},
		{"zero At", mut(func(s *signals.Signal) { s.At = time.Time{} }), 1},
		{"bad payload", mut(func(s *signals.Signal) { s.Payload = signals.Payload{"Bad": {}} }), 1},
		{"all bad", signals.Signal{Payload: signals.Payload{"Bad": {}}}, 4},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.s.Validate()
			wantKind(t, err, errs.Invalid)
			if n := len(validate.Fields(err)); n < tc.want {
				t.Errorf("FieldErrors = %d, want at least %d: %v", n, tc.want, err)
			}
		})
	}
}

func TestDeliveryValidate_Table(t *testing.T) {
	ok := signals.Delivery{Signal: validSignal(nameX), Subscriber: "beta", Attempt: 1}
	noErr(t, ok.Validate())
	bad := []struct {
		name string
		d    signals.Delivery
	}{
		{"bad signal", signals.Delivery{Signal: signals.Signal{}, Subscriber: "beta", Attempt: 1}},
		{"empty subscriber", signals.Delivery{Signal: validSignal(nameX), Subscriber: "", Attempt: 1}},
		{"bad subscriber", signals.Delivery{Signal: validSignal(nameX), Subscriber: "Beta_1", Attempt: 1}},
		{"attempt zero", signals.Delivery{Signal: validSignal(nameX), Subscriber: "beta", Attempt: 0}},
		{"attempt negative", signals.Delivery{Signal: validSignal(nameX), Subscriber: "beta", Attempt: -1}},
	}
	for _, tc := range bad {
		t.Run(tc.name, func(t *testing.T) { wantKind(t, tc.d.Validate(), errs.Invalid) })
	}
}

func TestOptionsValidate_AcceptsBoundaries(t *testing.T) {
	c := telemetrytest.New(t)
	for _, d := range []time.Duration{100 * time.Millisecond, time.Minute, 5 * time.Second} {
		o := baseOptions(c, clock.NewFake(time.Unix(0, 0)))
		o.HandlerTimeout = d
		noErr(t, o.Validate())
	}
}

func TestNew_BadOptions_ReportsEach(t *testing.T) {
	c := telemetrytest.New(t)
	good := func() signals.Options { return baseOptions(c, clock.NewFake(time.Unix(0, 0))) }
	tests := []struct {
		name string
		mod  func(*signals.Options)
		want []string
	}{
		{"nil logger", func(o *signals.Options) { o.Logger = nil }, []string{"signals.logger"}},
		{"nil tracer", func(o *signals.Options) { o.Tracer = nil }, []string{"signals.tracer"}},
		{"nil meter", func(o *signals.Options) { o.Meter = nil }, []string{"signals.meter"}},
		{"nil clock", func(o *signals.Options) { o.Clock = nil }, []string{"signals.clock"}},
		{"timeout too short", func(o *signals.Options) { o.HandlerTimeout = 99 * time.Millisecond }, []string{"signals.handler_timeout"}},
		{"timeout too long", func(o *signals.Options) { o.HandlerTimeout = time.Minute + time.Nanosecond }, []string{"signals.handler_timeout"}},
		{"timeout zero", func(o *signals.Options) { o.HandlerTimeout = 0 }, []string{"signals.handler_timeout"}},
		{"timeout negative", func(o *signals.Options) { o.HandlerTimeout = -time.Second }, []string{"signals.handler_timeout"}},
		{"all at once", func(o *signals.Options) { *o = signals.Options{} },
			[]string{"signals.logger", "signals.tracer", "signals.meter", "signals.clock", "signals.handler_timeout"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			o := good()
			tc.mod(&o)
			err := o.Validate()
			wantKind(t, err, errs.Invalid)
			got := fieldNames(err)
			for _, f := range tc.want {
				if !got[f] {
					t.Errorf("missing field %s in %v", f, got)
				}
			}
			if len(got) != len(tc.want) {
				t.Errorf("fields = %v, want %v", got, tc.want)
			}
			d, nerr := signals.New(o)
			wantKind(t, nerr, errs.Invalid)
			if d != nil {
				t.Error("New returned a dispatcher with an error")
			}
		})
	}
}

// failingMeter fails every Counter call with kind.
type failingMeter struct {
	telemetry.Meter
	kind errs.Kind
}

func (m failingMeter) Counter(string, ...telemetry.InstrumentOption) (telemetry.Counter, error) {
	return nil, errs.E("test.Counter", m.kind, context.DeadlineExceeded)
}

func TestNew_CounterCreationFails_ReturnsKind(t *testing.T) {
	c := telemetrytest.New(t)
	for _, k := range []errs.Kind{errs.Invalid, errs.Conflict} {
		o := baseOptions(c, clock.NewFake(time.Unix(0, 0)))
		o.Meter = failingMeter{Meter: o.Meter, kind: k}
		d, err := signals.New(o)
		wantKind(t, err, k)
		if d != nil {
			t.Error("dispatcher returned with error")
		}
	}
}

func TestNew_SameMeterScopeTwice_ReturnsKind(t *testing.T) {
	// F8 for real: counters of one meter scope are created once; a second New over the
	// same scope either shares them or fails with Invalid/Conflict, never panics.
	c := telemetrytest.New(t)
	o := baseOptions(c, clock.NewFake(time.Unix(0, 0)))
	_, err := signals.New(o)
	noErr(t, err)
	_, err = signals.New(o)
	if err != nil && !errs.Is(err, errs.Invalid) && !errs.Is(err, errs.Conflict) {
		t.Errorf("kind = %v", errs.KindOf(err))
	}
}
