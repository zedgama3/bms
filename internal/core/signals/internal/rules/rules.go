// Package rules holds the grammar, the scope check and the context markers that signals
// and signalstest share, so the real bus and the fake apply one rule. It imports lib only.
package rules

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/zedgama3/bms/internal/lib/validate"
)

// MaxDepth is how deep handlers may publish from handlers (signals.MaxDepth).
const MaxDepth = 8

// maxIdent is the longest segment of a signal name or payload key.
const maxIdent = 64

var (
	identRE = regexp.MustCompile(`^[a-z][a-z0-9]*(-[a-z0-9]+)*$`)
	ownerRE = regexp.MustCompile(`^[a-z][a-z0-9]{1,31}$`)
)

// Ident reports whether s is "[a-z][a-z0-9]*(-[a-z0-9]+)*" of at most 64 bytes.
func Ident(s string) bool { return len(s) <= maxIdent && identRE.MatchString(s) }

// Owner reports whether s is a module or core-area name: "[a-z][a-z0-9]{1,31}".
func Owner(s string) bool { return ownerRE.MatchString(s) }

// SplitName splits "<publisher>.<ident>"; ok is false unless both segments are idents.
func SplitName(n string) (publisher, rest string, ok bool) {
	publisher, rest, found := strings.Cut(n, ".")
	if !found || !Ident(publisher) || !Ident(rest) {
		return "", "", false
	}
	return publisher, rest, true
}

// Cap shortens s for an error message so a bad value cannot flood a log.
func Cap(s string) string {
	const limit = 80
	if len(s) <= limit {
		return s
	}
	return s[:limit] + "..."
}

// Scope is the set of names one owner declared.
type Scope struct {
	owner string
	pub   map[string]struct{}
	sub   map[string]struct{}
}

// NewScope checks owner and both lists as signals.Dispatcher.Scope documents (F9, F10) and
// returns the scope. The error is errs.Invalid with op as its operation.
func NewScope(op, owner string, publishes, subscribes []string) (*Scope, error) {
	acc := validate.Errors{Op: op}
	if !Owner(owner) {
		acc.Addf("owner", "malformed", "must be 2 to 32 characters of a-z and 0-9, starting with a letter")
	}
	s := &Scope{owner: owner, pub: map[string]struct{}{}, sub: map[string]struct{}{}}
	for i, n := range publishes {
		field := fmt.Sprintf("publishes[%d]", i)
		pub, _, ok := SplitName(n)
		switch {
		case !ok:
			acc.Addf(field, "malformed", "must be <publisher>.<ident>")
		case pub != owner:
			acc.Addf(field, "mismatch", "the publisher segment must be %q", owner)
		default:
			if _, dup := s.pub[n]; dup {
				acc.Addf(field, "duplicate", "listed more than once")
			}
			s.pub[n] = struct{}{}
		}
	}
	for i, n := range subscribes {
		field := fmt.Sprintf("subscribes[%d]", i)
		if _, _, ok := SplitName(n); !ok {
			acc.Addf(field, "malformed", "must be <publisher>.<ident>")
			continue
		}
		if _, dup := s.sub[n]; dup {
			acc.Addf(field, "duplicate", "listed more than once")
		}
		s.sub[n] = struct{}{}
	}
	if err := acc.Err(); err != nil {
		return nil, err //nolint:wrapcheck // Err already carries Op and errs.Invalid
	}
	return s, nil
}

// Owner returns the owner the scope was built for.
func (s *Scope) Owner() string { return s.owner }

// CanPublish reports whether name was declared in publishes.
func (s *Scope) CanPublish(name string) bool { _, ok := s.pub[name]; return ok }

// CanSubscribe reports whether name was declared in subscribes.
func (s *Scope) CanSubscribe(name string) bool { _, ok := s.sub[name]; return ok }

type depthKey struct{}

// WithDepth returns ctx marked as running inside a handler at nesting depth n (n >= 1).
func WithDepth(ctx context.Context, n int) context.Context {
	return context.WithValue(ctx, depthKey{}, n)
}

// Depth returns the handler nesting depth ctx carries; 0 outside any handler.
func Depth(ctx context.Context) int {
	n, ok := ctx.Value(depthKey{}).(int)
	if !ok {
		return 0
	}
	return n
}
