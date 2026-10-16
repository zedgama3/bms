package errs_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/zedgama3/bms/internal/lib/errs"
)

var allKinds = []struct {
	kind errs.Kind
	name string
}{
	{errs.Internal, "internal"},
	{errs.Invalid, "invalid"},
	{errs.Unauthenticated, "unauthenticated"},
	{errs.Permission, "permission"},
	{errs.NotFound, "not_found"},
	{errs.Conflict, "conflict"},
	{errs.Unavailable, "unavailable"},
	{errs.Timeout, "timeout"},
	{errs.Canceled, "canceled"},
	{errs.Unsupported, "unsupported"},
}

// F1
func TestE_NilErr_ReturnsNil(t *testing.T) {
	for _, k := range allKinds {
		if got := errs.E("op", k.kind, nil); got != nil {
			t.Errorf("E(op, %v, nil) = %v, want nil", k.kind, got)
		}
	}
}

// F2
func TestE_OutOfRangeKind_ClassifiesInternal(t *testing.T) {
	cause := errors.New("cause")
	for _, k := range []errs.Kind{errs.Kind(10), errs.Kind(200), errs.Kind(255)} {
		t.Run(k.String(), func(t *testing.T) {
			got := errs.E("op", k, cause)
			var e *errs.Error
			if !errors.As(got, &e) {
				t.Fatalf("E returned %T, want *errs.Error", got)
			}
			if e.Kind != errs.Internal {
				t.Errorf("recorded Kind = %v, want Internal", e.Kind)
			}
			if !errs.Is(got, errs.Internal) {
				t.Errorf("KindOf = %v, want Internal", errs.KindOf(got))
			}
		})
	}
}

// F3
func TestE_EmptyOp_OmitsPrefix(t *testing.T) {
	got := errs.E("", errs.Invalid, errors.New("boom"))
	if got == nil {
		t.Fatal("E returned nil")
	}
	if !errs.Is(got, errs.Invalid) {
		t.Errorf("KindOf = %v, want Invalid", errs.KindOf(got))
	}
	if got.Error() != "boom" {
		t.Errorf("Error() = %q, want %q", got.Error(), "boom")
	}
}

// F4
func TestE_Rekind_OuterKindWins(t *testing.T) {
	cause := errors.New("root")
	inner := errs.E("inner", errs.NotFound, cause)
	outer := errs.E("outer", errs.Invalid, inner)

	if !errs.Is(outer, errs.Invalid) {
		t.Errorf("KindOf(outer) = %v, want Invalid", errs.KindOf(outer))
	}
	if errs.Is(outer, errs.NotFound) {
		t.Error("Is(outer, NotFound) = true, want false")
	}
	// inner kind and message still reachable via Unwrap
	if got := errors.Unwrap(outer); got != inner { //nolint:errorlint // identity is the property under test
		t.Errorf("Unwrap(outer) = %v, want inner error", got)
	}
	if !errs.Is(errors.Unwrap(outer), errs.NotFound) {
		t.Error("inner kind not NotFound after Unwrap")
	}
	if !errors.Is(outer, cause) {
		t.Error("errors.Is(outer, cause) = false")
	}
}

// F5
func TestKindOf_Nil_ReturnsInternal(t *testing.T) {
	if got := errs.KindOf(nil); got != errs.Internal {
		t.Errorf("KindOf(nil) = %v, want Internal", got)
	}
}

// F6
func TestKindOf_Unkinded_ReturnsInternal(t *testing.T) {
	cases := []struct {
		name string
		err  error
	}{
		{"plain", errors.New("x")},
		{"wrapped", fmt.Errorf("ctx: %w", errors.New("x"))},
		{"joined", errors.Join(errors.New("a"), errors.New("b"))},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := errs.KindOf(tc.err); got != errs.Internal {
				t.Errorf("KindOf = %v, want Internal", got)
			}
		})
	}
}

// F7
func TestKindOf_DeadlineExceeded_ReturnsTimeout(t *testing.T) {
	cases := []struct {
		name string
		err  error
	}{
		{"bare", context.DeadlineExceeded},
		{"wrapped", fmt.Errorf("query: %w", context.DeadlineExceeded)},
		{"joined", errors.Join(errors.New("a"), context.DeadlineExceeded)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := errs.KindOf(tc.err); got != errs.Timeout {
				t.Errorf("KindOf = %v, want Timeout", got)
			}
		})
	}
}

// F8
func TestKindOf_Canceled_ReturnsCanceled(t *testing.T) {
	cases := []struct {
		name string
		err  error
	}{
		{"bare", context.Canceled},
		{"wrapped", fmt.Errorf("query: %w", context.Canceled)},
		{"joined", errors.Join(errors.New("a"), context.Canceled)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := errs.KindOf(tc.err); got != errs.Canceled {
				t.Errorf("KindOf = %v, want Canceled", got)
			}
		})
	}
}

// F9
func TestKindOf_KindedOutranksContextSentinel(t *testing.T) {
	cases := []struct {
		name     string
		err      error
		wantKind errs.Kind
	}{
		{"deadline under Unavailable", errs.E("op", errs.Unavailable, context.DeadlineExceeded), errs.Unavailable},
		{"canceled under Invalid", errs.E("op", errs.Invalid, context.Canceled), errs.Invalid},
		{"deadline under Internal", errs.E("op", errs.Internal, context.DeadlineExceeded), errs.Internal},
		{"through fmt", fmt.Errorf("x: %w", errs.E("op", errs.Permission, context.Canceled)), errs.Permission},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := errs.KindOf(tc.err); got != tc.wantKind {
				t.Errorf("KindOf = %v, want %v", got, tc.wantKind)
			}
		})
	}
}

// F10
func TestKindOf_ThroughFmtErrorf_PreservesKind(t *testing.T) {
	for _, k := range allKinds {
		t.Run(k.name, func(t *testing.T) {
			inner := errs.E("op", k.kind, errors.New("c"))
			wrapped := fmt.Errorf("outer: %w", fmt.Errorf("mid: %w", inner))
			if got := errs.KindOf(wrapped); got != k.kind {
				t.Errorf("KindOf = %v, want %v", got, k.kind)
			}
		})
	}
}

// F11
func TestKindOf_Joined_FirstKindedDepthFirst(t *testing.T) {
	nf := errs.E("a", errs.NotFound, errors.New("a"))
	cf := errs.E("b", errs.Conflict, errors.New("b"))
	plain := errors.New("plain")

	cases := []struct {
		name string
		err  error
		want errs.Kind
	}{
		{"first of two", errors.Join(nf, cf), errs.NotFound},
		{"reversed", errors.Join(cf, nf), errs.Conflict},
		{"skips unkinded first", errors.Join(plain, cf, nf), errs.Conflict},
		{"nested join depth first", errors.Join(errors.Join(plain, cf), nf), errs.Conflict},
		{"nested via fmt depth first", errors.Join(fmt.Errorf("w: %w", errors.Join(plain, cf)), nf), errs.Conflict},
		{"primary then close error", errors.Join(errs.E("q", errs.Unavailable, plain), errs.E("close", errs.Internal, plain)), errs.Unavailable},
		{"kinded after sentinel member", errors.Join(context.Canceled, cf), errs.Conflict},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := errs.KindOf(tc.err); got != tc.want {
				t.Errorf("KindOf = %v, want %v", got, tc.want)
			}
			if !errs.Is(tc.err, tc.want) {
				t.Errorf("Is(err, %v) = false", tc.want)
			}
		})
	}
}

// F12
func TestKindOf_Joined_Unkinded_UsesSentinel(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want errs.Kind
	}{
		{"deadline", errors.Join(errors.New("a"), context.DeadlineExceeded), errs.Timeout},
		{"deadline first", errors.Join(context.DeadlineExceeded, errors.New("a")), errs.Timeout},
		{"canceled", errors.Join(errors.New("a"), context.Canceled), errs.Canceled},
		{"canceled first", errors.Join(context.Canceled, errors.New("a")), errs.Canceled},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := errs.KindOf(tc.err); got != tc.want {
				t.Errorf("KindOf = %v, want %v", got, tc.want)
			}
		})
	}
}

// F13
func TestKindOf_OutOfRangeLiteral_ReturnsInternal(t *testing.T) {
	for _, k := range []errs.Kind{errs.Kind(10), errs.Kind(200), errs.Kind(255)} {
		t.Run(k.String(), func(t *testing.T) {
			err := &errs.Error{Op: "op", Kind: k, Err: errors.New("c")}
			if got := errs.KindOf(err); got != errs.Internal {
				t.Errorf("KindOf = %v, want Internal", got)
			}
			if !errs.Is(err, errs.Internal) {
				t.Error("Is(err, Internal) = false")
			}
		})
	}
	t.Run("joined literal first then kinded", func(t *testing.T) {
		bad := &errs.Error{Op: "op", Kind: errs.Kind(200), Err: errors.New("c")}
		good := errs.E("q", errs.Unavailable, errors.New("x"))
		if got := errs.KindOf(errors.Join(bad, good)); got != errs.Internal {
			t.Errorf("KindOf = %v, want Internal", got)
		}
	})
}

// F14
func TestIs_Nil_False(t *testing.T) {
	for _, k := range allKinds {
		if errs.Is(nil, k.kind) {
			t.Errorf("Is(nil, %v) = true, want false", k.kind)
		}
	}
	if errs.Is(nil, errs.Kind(200)) {
		t.Error("Is(nil, Kind(200)) = true, want false")
	}
}

// F15
func TestKindString_OutOfRange(t *testing.T) {
	cases := []struct {
		k    errs.Kind
		want string
	}{
		{errs.Kind(10), "Kind(10)"},
		{errs.Kind(200), "Kind(200)"},
		{errs.Kind(255), "Kind(255)"},
	}
	for _, tc := range cases {
		t.Run(tc.want, func(t *testing.T) {
			if got := tc.k.String(); got != tc.want {
				t.Errorf("String() = %q, want %q", got, tc.want)
			}
		})
	}
}

// F16
func TestErrorString_NilErr_UsesKindName(t *testing.T) {
	cases := []struct {
		name string
		err  *errs.Error
		want string
	}{
		{"op and kind", &errs.Error{Op: "pkg.Func", Kind: errs.NotFound}, "pkg.Func: not_found"},
		{"kind only", &errs.Error{Kind: errs.NotFound}, "not_found"},
		{"zero value", &errs.Error{}, "internal"},
		{"op and zero kind", &errs.Error{Op: "x"}, "x: internal"},
		{"op and out-of-range kind", &errs.Error{Op: "op", Kind: errs.Kind(200)}, "op: Kind(200)"},
		{"out-of-range kind only", &errs.Error{Kind: errs.Kind(200)}, "Kind(200)"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.err.Error(); got != tc.want {
				t.Errorf("Error() = %q, want %q", got, tc.want)
			}
			if tc.err.Unwrap() != nil {
				t.Error("Unwrap() != nil for literal with nil Err")
			}
		})
	}
}

func TestIs_AgreesWithKindOf(t *testing.T) {
	cause := errors.New("cause")
	for _, k := range allKinds {
		t.Run(k.name, func(t *testing.T) {
			err := errs.E("op", k.kind, cause)
			if got := errs.KindOf(err); got != k.kind {
				t.Errorf("KindOf = %v, want %v", got, k.kind)
			}
			for _, other := range allKinds {
				want := other.kind == k.kind
				if got := errs.Is(err, other.kind); got != want {
					t.Errorf("Is(err, %v) = %v, want %v", other.kind, got, want)
				}
			}
		})
	}
}

func TestKindString_AllKinds_Stable(t *testing.T) {
	if len(allKinds) != 10 {
		t.Fatalf("table has %d kinds, spec defines 10", len(allKinds))
	}
	for i, k := range allKinds {
		t.Run(k.name, func(t *testing.T) {
			if got := k.kind.String(); got != k.name {
				t.Errorf("String() = %q, want %q", got, k.name)
			}
			if int(k.kind) != i {
				t.Errorf("constant value = %d, want %d (declaration order)", k.kind, i)
			}
		})
	}
	var zero errs.Kind
	if zero != errs.Internal {
		t.Error("zero Kind is not Internal")
	}
}

func TestKindOf_RealContextDeadline(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 0)
	defer cancel()
	<-ctx.Done()
	if got := errs.KindOf(ctx.Err()); got != errs.Timeout {
		t.Errorf("KindOf(ctx.Err()) = %v, want Timeout", got)
	}
}

func TestKindOf_RealContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	<-ctx.Done()
	if got := errs.KindOf(ctx.Err()); got != errs.Canceled {
		t.Errorf("KindOf(ctx.Err()) = %v, want Canceled", got)
	}
}

func TestE_Chained_MessageIsOuterOpThenInner(t *testing.T) {
	cause := errors.New("connection refused")
	err := errs.E("outer", errs.Unavailable, errs.E("inner", errs.Unavailable, cause))
	if got, want := err.Error(), "outer: inner: connection refused"; got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}

func TestErrorsAs_FindsErrorThroughFmtErrorf(t *testing.T) {
	err := fmt.Errorf("ctx: %w", errs.E("pkg.Func", errs.Conflict, errors.New("dup")))
	var target *errs.Error
	if !errors.As(err, &target) {
		t.Fatal("errors.As did not find *errs.Error")
	}
	if target.Op != "pkg.Func" || target.Kind != errs.Conflict {
		t.Errorf("target = {Op:%q Kind:%v}, want {pkg.Func conflict}", target.Op, target.Kind)
	}
}

func TestE_WrapsCauseWithOpAndKind(t *testing.T) {
	cause := errors.New("cause")
	got := errs.E("volunteers.postgres.CreateShift", errs.Unavailable, cause)
	if got == nil {
		t.Fatal("E returned nil")
	}
	var e *errs.Error
	if !errors.As(got, &e) {
		t.Fatalf("errors.As failed for %T", got)
	}
	if e.Op != "volunteers.postgres.CreateShift" {
		t.Errorf("Op = %q", e.Op)
	}
	if e.Kind != errs.Unavailable {
		t.Errorf("Kind = %v, want Unavailable", e.Kind)
	}
	if e.Err != cause { //nolint:errorlint // identity is the property under test
		t.Errorf("Err = %v, want cause", e.Err)
	}
	if e.Unwrap() != cause { //nolint:errorlint // identity is the property under test
		t.Error("Unwrap() != cause")
	}
	if !errors.Is(got, cause) {
		t.Error("errors.Is(got, cause) = false")
	}
	if got.Error() != "volunteers.postgres.CreateShift: cause" {
		t.Errorf("Error() = %q", got.Error())
	}
}

// F17
func TestKindOf_BothSentinels_CanceledWins(t *testing.T) {
	plain := errors.New("plain")
	cases := []struct {
		name string
		err  error
	}{
		{"canceled then deadline", errors.Join(context.Canceled, context.DeadlineExceeded)},
		{"deadline then canceled", errors.Join(context.DeadlineExceeded, context.Canceled)},
		{"nested canceled inside", errors.Join(context.DeadlineExceeded, errors.Join(plain, context.Canceled))},
		{"nested deadline inside", errors.Join(errors.Join(plain, context.DeadlineExceeded), context.Canceled)},
		{"through fmt", errors.Join(fmt.Errorf("a: %w", context.DeadlineExceeded), fmt.Errorf("b: %w", context.Canceled))},
		{"single error wrapping both", fmt.Errorf("%w; %w", context.DeadlineExceeded, context.Canceled)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := errs.KindOf(tc.err); got != errs.Canceled {
				t.Errorf("KindOf = %v, want Canceled", got)
			}
		})
	}
}

// F18
func TestKindOf_TypedNilError_ReturnsInternal(t *testing.T) {
	var typedNil *errs.Error
	good := errs.E("q", errs.Unavailable, errors.New("x"))
	err := errors.Join(error(typedNil), good)
	if got := errs.KindOf(err); got != errs.Internal {
		t.Errorf("KindOf = %v, want Internal", got)
	}
}
