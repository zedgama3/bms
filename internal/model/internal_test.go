package model

import "testing"

func TestParseUUIDInternal_PlacesEveryGroup(t *testing.T) {
	got, ok := parseUUID("00010203-0405-0607-0809-0a0b0c0d0e0f")
	if !ok {
		t.Fatal("canonical form rejected")
	}
	for i, b := range got {
		if int(b) != i {
			t.Fatalf("byte %d = %d, want %d", i, b, i)
		}
	}
	if s := ID[person](got).String(); s != "00010203-0405-0607-0809-0a0b0c0d0e0f" {
		t.Fatalf("String = %q", s)
	}
}

func TestEmailCheckInternal_ByteLimitsAtBoundary(t *testing.T) {
	local64 := make([]byte, 64)
	for i := range local64 {
		local64[i] = 'a'
	}
	if err := Email(string(local64) + "@example.org").Validate(); err != nil {
		t.Fatalf("64-byte local part rejected: %v", err)
	}
	if err := Email(string(local64) + "a@example.org").Validate(); err == nil {
		t.Fatal("65-byte local part accepted")
	}
}
