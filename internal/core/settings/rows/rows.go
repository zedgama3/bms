// Package rows holds the record types settings' service shares with its postgres store. It
// imports nothing from the module, so both can use it.
package rows

// Setting is one row of core.setting: a key set away from its default. Exactly one of
// Value and Sealed is set (a Secret is sealed). Sealed is ciphertext and is never logged.
type Setting struct {
	Key    string
	Value  *string
	Sealed []byte
}

// Term is one row of core.terminology: one form of one term in one language.
type Term struct {
	Lang, Term, Form, Value string
}
