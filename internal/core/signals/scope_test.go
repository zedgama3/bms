package signals_test

import (
	"context"
	"strings"
	"testing"

	"github.com/zedgama3/bms/internal/core/signals"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/validate"
)

func TestScope_BadOwner_ReturnsInvalid(t *testing.T) {
	f := newFixture(t)
	for _, owner := range []string{"", "a", "A1", "ab_c", "1ab", "a-b", strings.Repeat("a", 33), "ab.c"} {
		b, err := f.disp.Scope(owner, nil, nil)
		wantKind(t, err, errs.Invalid)
		if b != nil {
			t.Errorf("owner %q: bus returned with error", owner)
		}
	}
	for _, owner := range []string{"ab", "a1", strings.Repeat("a", 32)} {
		if _, err := f.disp.Scope(owner, nil, nil); err != nil {
			t.Errorf("owner %q: %v", owner, err)
		}
	}
}

func TestScope_BadLists_ReturnsInvalid(t *testing.T) {
	tests := []struct {
		name     string
		pub, sub []signals.Name
		prefix   string
	}{
		{"publish invalid name", []signals.Name{"alpha"}, nil, "publishes"},
		{"publish other publisher", []signals.Name{"alpha.x", "beta.y"}, nil, "publishes"},
		{"publish repeated", []signals.Name{"alpha.x", "alpha.x"}, nil, "publishes"},
		{"subscribe invalid name", nil, []signals.Name{"Beta.y"}, "subscribes"},
		{"subscribe repeated", nil, []signals.Name{"beta.y", "beta.y"}, "subscribes"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			_, err := f.disp.Scope("alpha", tc.pub, tc.sub)
			wantKind(t, err, errs.Invalid)
			found := false
			for _, fe := range validate.Fields(err) {
				if strings.HasPrefix(fe.Field, tc.prefix) {
					found = true
				}
			}
			if !found {
				t.Errorf("no FieldError with prefix %q: %v", tc.prefix, err)
			}
			// a failed Scope does not consume the owner
			if _, err := f.disp.Scope("alpha", nil, nil); err != nil {
				t.Errorf("owner consumed by a failed Scope: %v", err)
			}
		})
	}
}

func TestScope_SubscribingToOwnPublisherIsAllowed(t *testing.T) {
	f := newFixture(t)
	_, err := f.disp.Scope("alpha", []signals.Name{nameX}, []signals.Name{nameX})
	noErr(t, err)
}

func TestScope_Twice_ReturnsConflict(t *testing.T) {
	f := newFixture(t)
	f.scope("alpha", nil, nil)
	_, err := f.disp.Scope("alpha", nil, nil)
	wantKind(t, err, errs.Conflict)
}

func TestScope_ReturnsIndependentBuses(t *testing.T) {
	f := newFixture(t)
	a := f.scope("alpha", []signals.Name{nameX}, nil)
	b := f.scope("beta", []signals.Name{nameOther}, []signals.Name{nameX})

	wantKind(t, a.Publish(context.Background(), nameOther, nil), errs.Invalid)
	wantKind(t, b.Publish(context.Background(), nameX, nil), errs.Invalid)
	wantKind(t, a.Subscribe(nameX, func(context.Context, signals.Signal) error { return nil }), errs.Invalid)
	noErr(t, b.Subscribe(nameX, func(context.Context, signals.Signal) error { return nil }))
	noErr(t, a.Publish(context.Background(), nameX, nil))
}

func TestSubscribe_Undeclared_ReturnsInvalid(t *testing.T) {
	f := newFixture(t)
	b := f.scope("beta", nil, []signals.Name{nameX})
	err := b.Subscribe(nameY, func(context.Context, signals.Signal) error { return nil })
	wantKind(t, err, errs.Invalid)
	err = b.Subscribe("not a name", func(context.Context, signals.Signal) error { return nil })
	wantKind(t, err, errs.Invalid)
}

func TestSubscribe_NilHandler_ReturnsInvalid(t *testing.T) {
	f := newFixture(t)
	b := f.scope("beta", nil, []signals.Name{nameX})
	wantKind(t, b.Subscribe(nameX, nil), errs.Invalid)
	// the failed call registered nothing: a real handler can still subscribe
	noErr(t, b.Subscribe(nameX, func(context.Context, signals.Signal) error { return nil }))
}

func TestSubscribe_Twice_ReturnsConflict(t *testing.T) {
	f := newFixture(t)
	b := f.scope("beta", nil, []signals.Name{nameX})
	h := func(context.Context, signals.Signal) error { return nil }
	noErr(t, b.Subscribe(nameX, h))
	wantKind(t, b.Subscribe(nameX, h), errs.Conflict)
}

func TestSubscribeAndScope_AfterClose_ReturnInternal(t *testing.T) {
	f := newFixture(t)
	b := f.scope("beta", nil, []signals.Name{nameX})
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()
	noErr(t, f.disp.Close(ctx))
	wantKind(t, b.Subscribe(nameX, func(context.Context, signals.Signal) error { return nil }), errs.Internal)
	_, err := f.disp.Scope("gamma", nil, nil)
	wantKind(t, err, errs.Internal)
}
