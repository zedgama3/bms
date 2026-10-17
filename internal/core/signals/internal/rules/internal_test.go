package rules

import (
	"context"
	"strings"
	"testing"
)

func TestIdent_Table(t *testing.T) {
	tests := []struct {
		in   string
		want bool
	}{
		{"a", true}, {"shift-completed", true}, {"a1-b2", true},
		{"", false}, {"1a", false}, {"A", false}, {"a_b", false}, {"a--b", false},
		{"a-", false}, {strings.Repeat("a", 64), true}, {strings.Repeat("a", 65), false},
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			if got := Ident(tc.in); got != tc.want {
				t.Errorf("Ident(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

func TestOwner_Table(t *testing.T) {
	for in, want := range map[string]bool{
		"hello": true, "ab": true, "a": false, "Hello": false, "a-b": false,
		strings.Repeat("a", 32): true, strings.Repeat("a", 33): false,
	} {
		if got := Owner(in); got != want {
			t.Errorf("Owner(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestDepth_RoundTrip(t *testing.T) {
	ctx := context.Background()
	if Depth(ctx) != 0 {
		t.Fatal("a bare ctx has depth 0")
	}
	if got := Depth(WithDepth(ctx, 3)); got != 3 {
		t.Errorf("Depth = %d, want 3", got)
	}
}

func TestScope_CanPublishAndSubscribe(t *testing.T) {
	s, err := NewScope("test", "alpha", []string{"alpha.did-it"}, []string{"beta.did-it"})
	if err != nil {
		t.Fatal(err)
	}
	if !s.CanPublish("alpha.did-it") || s.CanPublish("beta.did-it") {
		t.Error("publish set wrong")
	}
	if !s.CanSubscribe("beta.did-it") || s.CanSubscribe("alpha.did-it") {
		t.Error("subscribe set wrong")
	}
}
