package app

// Fixture for TestSeamTable_FlagsUnlistedType. It is parsed, never compiled.

type assembly struct{ d Doer }

type process struct{ a *assembly }

type Doer interface{ Do() }

// holder has a field of type *assembly under an unusual name.
type holder struct {
	x int
	z *assembly
}

type lazyThing struct{}

type plain struct{ n int }

type doer struct{}

func (doer) Do() {}

// makeDoer returns a closure-backed interface value that reads the assembly.
func makeDoer(a *assembly) Doer {
	f := func() Doer { return a.d }
	_ = f
	return doer{}
}

// methodDoer is the receiver shape: the assembly is the receiver, not a parameter.
func (a *assembly) methodDoer() Doer {
	f := func() Doer { return a.d }
	_ = f
	return doer{}
}

// closers returns an unnamed type and must stay unflagged.
func (a *assembly) closers() []Doer {
	f := func() Doer { return a.d }
	_ = f
	return nil
}
