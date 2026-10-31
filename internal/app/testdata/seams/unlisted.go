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

// noCapture has a literal that never uses the assembly and must stay unflagged.
func noCapture(a *assembly) Doer {
	f := func() Doer { return doer{} }
	_ = f
	return doer{}
}

// channelDoer passes the assembly on as a call argument inside the literal (C2).
func channelDoer(a *assembly) Doer {
	f := func() Doer { return channelOf(a) }
	_ = f
	return doer{}
}

// guardDoer builds a holder with the assembly in a composite literal (C2).
func guardDoer(a *assembly) Doer {
	f := func() Doer { return lazyGuard{a: a} }
	_ = f
	return doer{}
}

// makeFunc returns an unnamed func type (C3).
func makeFunc(a *assembly) func() error {
	return func() error { return a.err }
}
