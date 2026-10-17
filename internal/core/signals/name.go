package signals

import (
	"strings"

	"github.com/zedgama3/bms/internal/core/signals/internal/rules"
	"github.com/zedgama3/bms/internal/lib/validate"
)

// Name identifies a signal (D24): "<publisher>.<ident>", for example
// "volunteers.shift-completed" or "people.person-created". The publisher is the module's
// name or a core area. Both segments follow module-contract's ident grammar
// "[a-z][a-z0-9]*(-[a-z0-9]+)*", at most 64 bytes each. The second segment says what
// happened, in the past tense; a change of state is its own name
// ("registrations.application-approved"), never a name plus a status.
type Name string

// Validate fails with errs.Invalid unless n is "<ident>.<ident>" (F1).
func (n Name) Validate() error {
	if _, _, ok := rules.SplitName(string(n)); !ok {
		acc := validate.Errors{Op: "signals.Name.Validate"}
		acc.Addf("name", codeMalformed,
			"must be <publisher>.<ident>, each of lower-case letters, digits and single hyphens, at most 64 bytes")
		return acc.Err() //nolint:wrapcheck // Err already carries Op and errs.Invalid
	}
	return nil
}

// Publisher returns the first segment. Cannot fail; meaningful once Validate passes.
func (n Name) Publisher() string {
	pub, _, _ := strings.Cut(string(n), ".")
	return pub
}

const codeMalformed = "malformed"
