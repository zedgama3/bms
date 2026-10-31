package app

import (
	"github.com/zedgama3/bms/internal/audit"
	n "github.com/zedgama3/bms/internal/notify"
)

// Fixture for the stored-capture tests (5b). Parsed, never compiled.

type assembly struct{ name string }

func (a *assembly) actor() string { return a.name }

// storeLiteral stores a capturing literal in a project struct (keyed): always fails.
func (a *assembly) storeLiteral() error {
	_ = audit.Options{Actor: func() string { return a.name }}
	return nil
}

// storePositional stores a capturing literal positionally, under an import alias.
func storePositional(a *assembly) error {
	_ = n.Decl{func() string { return a.name }}
	return nil
}

// storeMethodValue stores the method value a.actor: candidate assembly.actor.
func (a *assembly) storeMethodValue() error {
	_ = audit.Options{Actor: a.actor}
	return nil
}

func (a *assembly) run()   {}
func (a *assembly) other() {}
func (a *assembly) later() {}

func register(f func()) {}

// storeElided stores captures in elided-type elements: audit.Decl, and *audit.Decl.
func (a *assembly) storeElided() error {
	_ = []audit.Decl{{Handler: func() { a.run() }}}
	_ = []*audit.Decl{{Handler: a.other}}
	return nil
}

// storeNested stores captures inside a capturing literal that is itself only an argument.
func (a *assembly) storeNested() error {
	register(func() { _ = n.Options{Channel: func() { a.run() }} })
	register(func() { _ = audit.Options{Actor: a.later} })
	return nil
}
