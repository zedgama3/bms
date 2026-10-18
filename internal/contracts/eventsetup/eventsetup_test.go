package eventsetup_test

import (
	"context"
	"errors"
	"go/build"
	"strings"
	"testing"

	"github.com/zedgama3/bms/internal/contracts/eventsetup"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
)

// providerFunc adapts a function to the contract, as a module's own type would implement it.
type providerFunc func(ctx context.Context, req eventsetup.Request) (eventsetup.Result, error)

func (f providerFunc) SetUp(ctx context.Context, req eventsetup.Request) (eventsetup.Result, error) {
	return f(ctx, req)
}

var _ eventsetup.Provider = providerFunc(nil)

func TestPoint_IsTheSpecName(t *testing.T) {
	if eventsetup.Point != "events.event-setup" {
		t.Errorf("Point = %q, want events.event-setup", eventsetup.Point)
	}
}

// The extension point accepts a Provider (app's Accepts func): a value that implements SetUp
// is one, anything else is not.
func TestProvider_AcceptsOnlyImplementers(t *testing.T) {
	accepts := func(v any) bool { _, ok := v.(eventsetup.Provider); return ok }
	if !accepts(providerFunc(nil)) {
		t.Error("a SetUp implementer is not accepted")
	}
	for name, v := range map[string]any{"nil": nil, "string": "x", "struct": struct{}{}, "func": func() {}} {
		if accepts(v) {
			t.Errorf("%s is accepted as a Provider", name)
		}
	}
}

// The contract passes IDs and counts, and an error's kind survives the call.
func TestProvider_RequestResultAndErrorKinds(t *testing.T) {
	ev, from := model.NewID[model.EventID](), model.NewID[model.EventID]()
	var got eventsetup.Request
	p := providerFunc(func(_ context.Context, req eventsetup.Request) (eventsetup.Result, error) {
		got = req
		return eventsetup.Result{Items: 7}, nil
	})
	res, err := p.SetUp(t.Context(), eventsetup.Request{Event: ev, CopyFrom: from})
	if err != nil || res.Items != 7 {
		t.Fatalf("SetUp = %+v, %v", res, err)
	}
	if got.Event != ev || got.CopyFrom != from {
		t.Errorf("request = %+v", got)
	}

	denied := providerFunc(func(context.Context, eventsetup.Request) (eventsetup.Result, error) {
		return eventsetup.Result{}, errs.E("test", errs.Permission, errors.New("no"))
	})
	_, err = denied.SetUp(t.Context(), eventsetup.Request{Event: ev, CopyFrom: from})
	if !errs.Is(err, errs.Permission) {
		t.Errorf("kind = %v, want Permission", errs.KindOf(err))
	}
}

func TestResult_ZeroItems_IsAValidOutcome(t *testing.T) {
	var r eventsetup.Result
	if r.Items != 0 {
		t.Errorf("zero Result has %d items", r.Items)
	}
}

// The contract imports only the standard library and model: never events (a module must
// not need it), another core service, a module or the shell.
func TestImports_OnlyStdlibAndModel(t *testing.T) {
	pkg, err := build.ImportDir(".", 0)
	if err != nil {
		t.Fatalf("ImportDir: %v", err)
	}
	for _, imp := range pkg.Imports {
		if !strings.Contains(imp, ".") { // standard library
			continue
		}
		if imp != "github.com/zedgama3/bms/internal/model" {
			t.Errorf("the contract imports %s; only model is allowed", imp)
		}
	}
}
