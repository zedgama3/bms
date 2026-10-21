package api_test

import (
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/core/module"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/httpx"

	"github.com/zedgama3/bms/internal/api"
)

// api.md F61: a route's MaxBodyBytes or Timeout above its ceiling (or negative) is Invalid
// at New, naming the route.
func TestNew_RouteCapAboveCeiling_ReturnsInvalid(t *testing.T) {
	tests := []struct {
		name string
		mut  func(*module.Route)
	}{
		{"body above 1 GiB", func(r *module.Route) { r.MaxBodyBytes = module.MaxRouteBodyBytes + 1 }},
		{"body negative", func(r *module.Route) { r.MaxBodyBytes = -1 }},
		{"timeout above 10 minutes", func(r *module.Route) { r.Timeout = module.MaxRouteTimeout + time.Nanosecond }},
		{"timeout negative", func(r *module.Route) { r.Timeout = -time.Second }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rt := route("POST", "/up", module.AccessPublic, textHandler("x"))
			tc.mut(&rt)
			// Build the set by hand: setOf would be fine too, but Plan.Build is not under test.
			set := module.ModuleRoutes{Module: "alpha", Routes: []module.Route{rt}, OpenAPI: fragmentFor(t, []module.Route{rt}, nil)}
			r := newRig(t, withRoutes(set))
			if _, err := api.New(r.opts); !errs.Is(err, errs.Invalid) {
				t.Fatalf("New = %v, want Invalid", err)
			}
		})
	}
}

// The ceilings themselves are accepted.
func TestNew_RouteCapAtCeiling_Accepted(t *testing.T) {
	rt := route("POST", "/up", module.AccessPublic, textHandler("x"))
	rt.MaxBodyBytes, rt.Timeout = module.MaxRouteBodyBytes, module.MaxRouteTimeout
	r := newRig(t, withRoutes(setOf(t, "alpha", rt)))
	if _, err := api.New(r.opts); err != nil {
		t.Fatalf("New = %v", err)
	}
}

// api.md F62: a route's limits are raised for that route only and never lowered.
func TestPipeline_RouteCapRaised_AppliesRouteLimits(t *testing.T) {
	handler := func(w http.ResponseWriter, r *http.Request) {
		b, err := io.ReadAll(r.Body)
		if err != nil {
			httpx.Fail(w, r, err)
			return
		}
		left := time.Duration(0)
		if d, ok := r.Context().Deadline(); ok {
			left = time.Until(d)
		}
		w.Header().Set("X-Left-Ms", strconv.FormatInt(left.Milliseconds(), 10))
		w.Header().Set("X-Read", strconv.Itoa(len(b)))
		w.WriteHeader(http.StatusOK)
	}
	raised := route("POST", "/raised", module.AccessPublic, handler)
	raised.MaxBodyBytes, raised.Timeout = 1<<20, 5*time.Minute
	lowered := route("POST", "/lowered", module.AccessPublic, handler)
	lowered.MaxBodyBytes, lowered.Timeout = 100, time.Second // below the shell's: ignored
	plain := route("POST", "/plain", module.AccessPublic, handler)

	r := newRig(t, withRoutes(setOf(t, "alpha", raised, lowered, plain)), func(o *api.Options) {
		o.MaxBodyBytes = 1024
		o.RequestTimeout = 30 * time.Second
	})
	big := strings.Repeat("a", 4000)
	small := strings.Repeat("a", 500)
	left := func(t *testing.T, rec interface{ Header() http.Header }) time.Duration {
		t.Helper()
		ms, err := strconv.Atoi(rec.Header().Get("X-Left-Ms"))
		if err != nil {
			t.Fatalf("X-Left-Ms: %v", err)
		}
		return time.Duration(ms) * time.Millisecond
	}

	t.Run("raised route takes a body over the shell cap and has the longer deadline", func(t *testing.T) {
		rec := r.do("POST", "/api/alpha/raised", body("application/octet-stream", big))
		if rec.Code != 200 {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		if d := left(t, rec); d < 4*time.Minute {
			t.Errorf("deadline left = %v, want about 5m", d)
		}
	})
	t.Run("raised route still has its own cap", func(t *testing.T) {
		huge := strings.Repeat("a", 1<<20+1)
		wantProblem(t, r.do("POST", "/api/alpha/raised", body("application/octet-stream", huge)), 413, httpx.CodePayloadTooLarge)
	})
	t.Run("plain route keeps the shell limits", func(t *testing.T) {
		wantProblem(t, r.do("POST", "/api/alpha/plain", body("application/octet-stream", big)), 413, httpx.CodePayloadTooLarge)
		rec := r.do("POST", "/api/alpha/plain", body("application/octet-stream", small))
		if rec.Code != 200 {
			t.Fatalf("status = %d", rec.Code)
		}
		if d := left(t, rec); d > 31*time.Second || d < 20*time.Second {
			t.Errorf("deadline left = %v, want about 30s", d)
		}
	})
	t.Run("a lower route value never lowers the shell's", func(t *testing.T) {
		rec := r.do("POST", "/api/alpha/lowered", body("application/octet-stream", small))
		if rec.Code != 200 {
			t.Fatalf("a 500-byte body on a route with cap 100 = %d, want 200 (shell cap 1024)", rec.Code)
		}
		if d := left(t, rec); d < 20*time.Second {
			t.Errorf("deadline left = %v, want about 30s", d)
		}
	})
}
