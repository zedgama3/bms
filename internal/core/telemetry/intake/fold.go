package intake

import (
	"fmt"
	"sync"

	"github.com/zedgama3/bms/internal/core/telemetry"
	"github.com/zedgama3/bms/internal/lib/errs"
)

// The tokens a folded or unknown value becomes (telemetry T1.3, T1.5).
const (
	OtherToken     = "_other"       // a value outside the allowlist, or beyond the fold cap
	EntryToken     = "_entry"       // a screen view that began the page load
	unmatchedRoute = "/{unmatched}" // an api.route that is not a mounted pattern (api decision 10)
)

// Fold caps, used when an allowlist is empty (a development server with no manifest).
const (
	MaxDistinctScreens = 300
	MaxDistinctActions = 500
)

// folder bounds the values of one attribute. With a non-empty allowlist a value passes only
// if listed; with an empty one the first max distinct values are admitted for the life of the
// process (F65, F66). Safe for concurrent use; it holds a mutex and is never copied.
type folder struct {
	allow map[string]struct{} // nil: the cap applies
	extra map[string]struct{} // always admitted (EntryToken, unmatchedRoute)
	max   int

	mu   sync.Mutex
	seen map[string]struct{}
}

// setup prepares f in place. It fails with errs.Invalid if an entry is not a token (F51, F71).
func (f *folder) setup(what string, list []string, limit int, extra ...string) error {
	f.max = limit
	f.seen = map[string]struct{}{}
	f.extra = map[string]struct{}{}
	for _, e := range extra {
		f.extra[e] = struct{}{}
	}
	if len(list) == 0 {
		return nil
	}
	f.allow = make(map[string]struct{}, len(list))
	for i, e := range list {
		if !telemetry.IsToken(e) {
			return errs.E("intake.allowlist", errs.Invalid, fmt.Errorf("%s[%d] is not a token", what, i))
		}
		f.allow[e] = struct{}{}
	}
	return nil
}

// fold returns v itself, or OtherToken and true when v is outside the allowlist or the cap.
func (f *folder) fold(v string) (out string, folded bool) {
	if _, ok := f.extra[v]; ok {
		return v, false
	}
	if f.allow != nil {
		if _, ok := f.allow[v]; ok {
			return v, false
		}
		return OtherToken, true
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.seen[v]; ok {
		return v, false
	}
	if len(f.seen) >= f.max {
		return OtherToken, true
	}
	f.seen[v] = struct{}{}
	return v, false
}
