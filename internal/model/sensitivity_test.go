package model_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
)

func TestSensitivity_NamesRoundTrip(t *testing.T) {
	tests := []struct {
		s    model.Sensitivity
		name string
		n    uint8
	}{
		{model.SensitivityPublic, "public", 1},
		{model.SensitivityInternal, "internal", 2},
		{model.SensitivityPersonal, "personal", 3},
		{model.SensitivitySensitive, "sensitive", 4},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if uint8(tc.s) != tc.n {
				t.Errorf("value %d", tc.s)
			}
			if tc.s.String() != tc.name {
				t.Errorf("String %q", tc.s.String())
			}
			got, err := model.ParseSensitivity(tc.name)
			if err != nil || got != tc.s {
				t.Errorf("parse %v %v", got, err)
			}
			if err := tc.s.Validate(); err != nil {
				t.Error(err)
			}
		})
	}
}

func TestSensitivity_String_OutOfRange(t *testing.T) {
	if got := model.Sensitivity(0).String(); got != "Sensitivity(0)" {
		t.Error(got)
	}
	if got := model.Sensitivity(9).String(); got != "Sensitivity(9)" {
		t.Error(got)
	}
}

func TestSensitivity_Ordering(t *testing.T) {
	if model.SensitivityPublic >= model.SensitivityInternal ||
		model.SensitivityInternal >= model.SensitivityPersonal ||
		model.SensitivityPersonal >= model.SensitivitySensitive {
		t.Fatal("ordering broken")
	}
}

func TestParseSensitivity_Unknown_ReturnsInvalid(t *testing.T) {
	for _, in := range []string{"", "Public", "PUBLIC", " public", "public ", "secret", "sensitive\n", "1", "Sensitivity(1)"} {
		_, err := model.ParseSensitivity(in)
		if !errs.Is(err, errs.Invalid) {
			t.Errorf("%q: %v", in, err)
			continue
		}
	}
	_, err := model.ParseSensitivity("zz-leak-marker")
	assertNoEcho(t, err, "zz-leak-marker")
	// the reason lists the four names
	_, err = model.ParseSensitivity("nope")
	for _, n := range []string{"public", "internal", "personal", "sensitive"} {
		if !strings.Contains(err.Error(), n) {
			t.Errorf("error should list %q: %v", n, err)
		}
	}
}

func TestSensitivityValidate_ZeroAndOutOfRange_ReturnInvalid(t *testing.T) {
	for _, s := range []model.Sensitivity{0, 5, 200, 255} {
		if err := s.Validate(); !errs.Is(err, errs.Invalid) {
			t.Errorf("%d: %v", s, err)
		}
	}
}

func TestReadIsAudited_Table(t *testing.T) {
	tests := []struct {
		name string
		s    model.Sensitivity
		self bool
		want bool
	}{
		{"public other", model.SensitivityPublic, false, false},
		{"public self", model.SensitivityPublic, true, false},
		{"internal other", model.SensitivityInternal, false, false},
		{"internal self", model.SensitivityInternal, true, false},
		{"personal other", model.SensitivityPersonal, false, true},
		{"personal self", model.SensitivityPersonal, true, false},
		{"sensitive other", model.SensitivitySensitive, false, true},
		{"sensitive self", model.SensitivitySensitive, true, true},
		{"zero other", 0, false, true},
		{"zero self", 0, true, true},
		{"out of range other", 9, false, true},
		{"out of range self", 9, true, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.s.ReadIsAudited(tc.self); got != tc.want {
				t.Errorf("got %v want %v", got, tc.want)
			}
		})
	}
}

func TestSensitivityMarshalText_Invalid_ReturnsInvalid(t *testing.T) {
	for _, s := range []model.Sensitivity{0, 5, 255} {
		if _, err := s.MarshalText(); !errs.Is(err, errs.Invalid) {
			t.Errorf("%d: %v", s, err)
		}
	}
	b, err := model.SensitivityPersonal.MarshalText()
	if err != nil || string(b) != "personal" {
		t.Errorf("%q %v", b, err)
	}
}

func TestSensitivityUnmarshalText_Bad_ReturnsInvalidAndLeavesValue(t *testing.T) {
	for _, in := range []string{"", "Public", "bogus", " internal"} {
		s := model.SensitivityPersonal
		if err := s.UnmarshalText([]byte(in)); !errs.Is(err, errs.Invalid) {
			t.Errorf("%q: %v", in, err)
		}
		if s != model.SensitivityPersonal {
			t.Errorf("%q changed value", in)
		}
	}
}

func TestSensitivityUnmarshalText_NilReceiver_ReturnsInternal(t *testing.T) {
	var p *model.Sensitivity
	if err := p.UnmarshalText([]byte("public")); !errs.Is(err, errs.Internal) {
		t.Fatalf("got %v", err)
	}
}

func TestSensitivityJSON_RoundTrip(t *testing.T) {
	type doc struct {
		Level model.Sensitivity `json:"level"`
	}
	for _, s := range []model.Sensitivity{1, 2, 3, 4} {
		b, err := json.Marshal(doc{s})
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(b), `"`+s.String()+`"`) {
			t.Fatalf("json %s", b)
		}
		var out doc
		if err := json.Unmarshal(b, &out); err != nil || out.Level != s {
			t.Fatalf("round trip %v %v", out, err)
		}
	}
	if _, err := json.Marshal(doc{}); err == nil {
		t.Error("marshal of unset level should fail")
	}
	var out doc
	if err := json.Unmarshal([]byte(`{"level":"secret"}`), &out); err == nil {
		t.Error("bad name should fail")
	}
}
