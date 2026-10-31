package app

import (
	"net/http"
	"sync"

	"github.com/zedgama3/bms/internal/audit"
)

// Fixture for TestSeamTable_IgnoresWiring (rule 6). Parsed, never compiled.

type assembly struct{ name string }

func (a *assembly) run() {}

func (a *assembly) actor() string { return a.name }

type part struct{ start func() }

func register(f func()) {}

// None of these is a candidate, and no literal fails.
func (a *assembly) wire() error {
	_ = part{start: a.run}                                                                                           // app-declared struct
	_ = part{func() { a.run() }}                                                                                     // app-declared struct, literal
	register(a.run)                                                                                                  // call argument
	register(func() { a.run() })                                                                                     // call argument, literal
	_ = http.Server{ConnState: nil, Handler: http.HandlerFunc(func(http.ResponseWriter, *http.Request) { a.run() })} // stdlib, nested in a call
	_ = http.Server{ErrorLog: nil, BaseContext: func(net.Listener) context.Context { a.run(); return nil }}          // stdlib composite literal
	_ = sync.Once{}
	_ = []part{{start: a.run}}              // elided app type
	_ = []part{{start: func() { a.run() }}} // elided app type, literal
	_ = audit.Options{Actor: wrap(a.actor)} // package struct, but the value is a call
	f := a.run                              // local
	_ = f
	go func() { a.run() }() // goroutine
	return nil
}
