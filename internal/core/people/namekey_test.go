package people_test

import (
	"testing"

	"github.com/zedgama3/bms/internal/core/people"
)

// F82: the rule for "the same playa name". Every name here is invented.
func TestNameKey_Table(t *testing.T) {
	groups := map[string][]string{
		"one word":    {" Dusty ", "DUSTY", "dusty", " dusty ", "Ｄusty"}, // fullwidth D folds under NFKC
		"two words":   {"Dusty Roads", "Dusty  Roads", "Dusty\tRoads", "  DUSTY   roads\t", "dusty roads"},
		"accent e":    {"é", "é", "É"}, // precomposed, decomposed, upper
		"ligature":    {"ﬁre", "Fire"},  // NFKC expands the fi ligature
		"empty":       {"", "   ", "\t \n"},
		"other names": {"Dusty Road", "Dust y"},
	}
	key := func(s string) string { return people.NameKey(s) }
	for name, g := range groups {
		if name == "other names" {
			continue
		}
		t.Run(name, func(t *testing.T) {
			want := key(g[0])
			for _, s := range g {
				if got := key(s); got != want {
					t.Errorf("NameKey(%q) = %q, want %q (the key of %q)", s, got, want, g[0])
				}
			}
		})
	}
	t.Run("blank is the empty key", func(t *testing.T) {
		for _, s := range groups["empty"] {
			if got := key(s); got != "" {
				t.Errorf("NameKey(%q) = %q, want empty", s, got)
			}
		}
	})
	t.Run("lower case, one space", func(t *testing.T) {
		if got := key("  Dusty \t  Roads "); got != "dusty roads" {
			t.Errorf("got %q", got)
		}
	})
	t.Run("different names differ", func(t *testing.T) {
		seen := map[string]string{}
		for _, s := range []string{"dusty", "Dusty Roads", "Dusty Road", "Dust y", "é", "e", "fire"} {
			k := key(s)
			if prev, ok := seen[k]; ok && (prev != "é" || s != "e") {
				t.Errorf("%q and %q share key %q", prev, s, k)
			}
			seen[k] = s
		}
		if key("é") == key("e") {
			t.Error("an accent was stripped: the key must keep diacritics (P1.2, not taken)")
		}
	})
	t.Run("idempotent and pure", func(t *testing.T) {
		for _, s := range []string{"Dusty  Roads", "É", "Ｄusty", ""} {
			k := key(s)
			if key(k) != k {
				t.Errorf("NameKey is not idempotent on %q", s)
			}
			if key(s) != k {
				t.Errorf("NameKey is not deterministic on %q", s)
			}
		}
	})
}
