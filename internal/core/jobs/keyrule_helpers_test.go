//go:build integration

package jobs_test

import (
	"encoding/json"
	"os"
	"testing"
)

// keyRuleCase is one row of the key rule table (jobs.md J1): the existing row's status, the
// request made for its (name, key), what the handler returns if the row is running, and the
// row that must result. The same file feeds the PostgreSQL suite and the jobstest Fake, so neither
// can drift from the other.
type keyRuleCase struct {
	Name      string `json:"name"`
	Existing  string `json:"existing"` // queued, running, done, failed, canceled
	Request   string `json:"request"`  // before: made before any run; during: made while running; none
	Handler   string `json:"handler"`  // ok, fail (Invalid), retry (Unavailable); "" when not running
	Status    string `json:"status"`   // the status the row ends in
	RunAt     string `json:"runAt"`    // request, original or later
	Attempts  int    `json:"attempts"`
	FakeSkips bool   `json:"fakeSkips"` // a state the Fake does not model (cancel, backoff)
}

// loadKeyRuleCases reads the shared table.
func loadKeyRuleCases(t testing.TB) []keyRuleCase {
	t.Helper()
	b, err := os.ReadFile("internal/rules/testdata/keyrule.json")
	if err != nil {
		t.Fatalf("read key rule table: %v", err)
	}
	var cs []keyRuleCase
	if err := json.Unmarshal(b, &cs); err != nil {
		t.Fatalf("decode key rule table: %v", err)
	}
	if len(cs) == 0 {
		t.Fatal("empty key rule table")
	}
	return cs
}
