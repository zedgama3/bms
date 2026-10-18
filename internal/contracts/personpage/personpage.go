// Package personpage is the shape of the person-page extension point: what a module
// contributes to a person's page and how people asks for it. It holds no implementation
// (07-abstractions, "contracts"), so a module imports this package and never people.
package personpage

import (
	"context"
	"time"

	"github.com/zedgama3/bms/internal/model"
)

// Point is the extension point's name. app declares it in Env.CoreExtensionPoints with
// Accepts = func(v any) bool { _, ok := v.(Provider); return ok }.
const Point = "people.person-page"

// SectionTimeout is how long people waits for one provider before it marks the provider's
// section Failed. A provider that needs longer is too slow for a page and should answer
// from a summary.
const SectionTimeout = 2 * time.Second

// Item is one line in a section. Label is a message key in the contributing module's
// catalogue; Value is data already authorized for the viewer; Href is a shell route or "".
type Item struct {
	Label string
	Value string
	Href  string
}

// Section is one module's block on a person page. Key is "<module>.<ident>" and names the
// title key module.<module>.section.<ident>.title. Failed is set by people when the
// provider failed or timed out; Items is then empty.
type Section struct {
	Key    string
	Items  []Item
	Failed bool
}

// Provider is what a module contributes. It receives the viewer through
// identity.PersonFrom(ctx) and must return only what that viewer may see of each person,
// auditing its own personal and sensitive reads (D19: the provider checks permissions for
// the person asking). Batch: one call per page, len(people) >= 1. A person with nothing to
// show is simply absent from the map. Fails with the module's own kinds; people turns any
// error into Section{Failed: true}.
type Provider interface {
	Sections(ctx context.Context, people []model.PersonID) (map[model.PersonID][]Section, error)
}
