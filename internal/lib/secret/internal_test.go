package secret

import "testing"

// These tests need the unexported field: they pin the pointer shape that decision 1 of
// lib-secret.md relies on.

func TestNew_EmptyHasNilPointer(t *testing.T) {
	if s := New(""); s.p != nil {
		t.Fatalf("New(\"\").p = %v, want nil", s.p)
	}
	if s := New("x"); s.p == nil {
		t.Fatal("New(\"x\").p is nil, want a pointer")
	}
}

func TestIsSet_PointerToEmptyString_NotSet(t *testing.T) {
	empty := ""
	s := Secret{p: &empty}
	if s.IsSet() {
		t.Error("IsSet() = true for a pointer to an empty string")
	}
	if got := s.String(); got != Unset {
		t.Errorf("String() = %q, want %q", got, Unset)
	}
	if got := s.Value(); got != "" {
		t.Errorf("Value() = %q, want empty", got)
	}
}

func TestUnmarshalText_EmptyResetsToNilPointer(t *testing.T) {
	s := New("keep")
	if err := s.UnmarshalText(nil); err != nil {
		t.Fatalf("UnmarshalText(nil): %v", err)
	}
	if s.p != nil {
		t.Errorf("p = %v after empty text, want nil", s.p)
	}
}

func TestUnmarshalText_Failure_LeavesReceiverUnchanged(t *testing.T) {
	s := New("keep")
	before := s.p
	if err := s.UnmarshalText([]byte(Redacted)); err == nil {
		t.Fatal("want error for the Redacted marker")
	}
	if s.p != before || s.Value() != "keep" {
		t.Error("receiver changed on failure")
	}
}
