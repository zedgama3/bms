// Package greeter is the contract hello provides and the shape of its banner extension
// point. It holds no implementation (07-abstractions, "contracts").
package greeter

import "context"

// Name is the capability's name; it converts to module.Capability.
const Name = "greeter"

// Greeter returns the regional's greeting text. It is public text, so there is no actor to
// check and nothing to batch; a real capability takes batches and checks permissions for
// the actor in ctx (module-contract decision 15).
type Greeter interface {
	// Greeting fails with Canceled/Timeout when ctx is done, and with the settings
	// store's kind once settings exist (M2); in M0 it cannot fail otherwise.
	Greeting(ctx context.Context) (string, error)
}

// Banner is one contribution to the extension point "hello.banner": a line the hello
// screen shows for the contributing module. Key is a full message key owned by the
// contributor, "module.<contributor>.banner.<ident>"; hello checks the prefix at Start.
type Banner struct {
	Key string
}
