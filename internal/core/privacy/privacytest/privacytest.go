// Package privacytest asserts on a response body in tests: that a hidden field is absent
// at any depth and that a visible one is present.
package privacytest

import (
	"encoding/json"
	"testing"
)

// AssertHidden fails t if any of names is a key anywhere in the JSON body (objects are
// walked; arrays too). A body that is not JSON fails t.
func AssertHidden(t testing.TB, body []byte, names ...string) {
	t.Helper()
	var doc any
	if err := json.Unmarshal(body, &doc); err != nil {
		t.Fatalf("privacytest.AssertHidden: the body is not JSON: %v", err)
		return
	}
	want := make(map[string]bool, len(names))
	for _, n := range names {
		want[n] = true
	}
	found := map[string]bool{}
	collectKeys(doc, want, found)
	for _, n := range names {
		if found[n] {
			t.Errorf("privacytest.AssertHidden: field %q is present in the body", n)
		}
	}
}

// AssertPresent fails t if any of names is absent at the top level of the JSON body. A
// body that is not a JSON object fails t.
func AssertPresent(t testing.TB, body []byte, names ...string) {
	t.Helper()
	var top map[string]json.RawMessage
	if err := json.Unmarshal(body, &top); err != nil {
		t.Fatalf("privacytest.AssertPresent: the body is not a JSON object: %v", err)
		return
	}
	for _, n := range names {
		if _, ok := top[n]; !ok {
			t.Errorf("privacytest.AssertPresent: field %q is absent from the body", n)
		}
	}
}

// collectKeys records into found every key of doc, at any depth, that is in want.
func collectKeys(doc any, want, found map[string]bool) {
	switch v := doc.(type) {
	case map[string]any:
		for k, child := range v {
			if want[k] {
				found[k] = true
			}
			collectKeys(child, want, found)
		}
	case []any:
		for _, child := range v {
			collectKeys(child, want, found)
		}
	}
}
