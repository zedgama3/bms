package model_test

import (
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/validate"
	"github.com/zedgama3/bms/internal/model"
)

type shift struct{}

type ShiftID = model.ID[shift]

const sample = "0190a1b2-c3d4-4e5f-8a6b-7c8d9e0f1a2b"

func TestNewID_IsVersion4Variant10AndNonZero(t *testing.T) {
	for range 200 {
		id := model.NewID[model.PersonID]()
		if id.IsZero() {
			t.Fatal("zero id")
		}
		if id[6]>>4 != 4 {
			t.Fatalf("version nibble %x", id[6]>>4)
		}
		if id[8]>>6 != 0b10 {
			t.Fatalf("variant bits %b", id[8]>>6)
		}
	}
}

func TestNewID_DistinctAcross10000(t *testing.T) {
	seen := make(map[model.PersonID]struct{}, 10000)
	for range 10000 {
		id := model.NewID[model.PersonID]()
		if _, dup := seen[id]; dup {
			t.Fatal("duplicate id")
		}
		seen[id] = struct{}{}
	}
}

func TestNewID_ConcurrentUse(t *testing.T) {
	var wg sync.WaitGroup
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 200 {
				if model.NewID[model.EventID]().IsZero() {
					t.Error("zero id")
				}
			}
		}()
	}
	wg.Wait()
}

func TestParseID_RoundTripsString(t *testing.T) {
	tests := []struct{ name, in, want string }{
		{"lower", sample, sample},
		{"upper", strings.ToUpper(sample), sample},
		{"mixed", "0190A1b2-C3d4-4E5f-8a6B-7c8D9e0F1a2b", sample},
		{"nil uuid", "00000000-0000-0000-0000-000000000000", "00000000-0000-0000-0000-000000000000"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			id, err := model.ParseID[model.PersonID](tc.in)
			if err != nil {
				t.Fatal(err)
			}
			if id.String() != tc.want {
				t.Fatalf("got %s want %s", id, tc.want)
			}
		})
	}
}

func TestIDString_IsLowercaseCanonical(t *testing.T) {
	id := model.NewID[model.AccountID]()
	s := id.String()
	if len(s) != 36 || s != strings.ToLower(s) {
		t.Fatalf("bad form %q", s)
	}
	for _, i := range []int{8, 13, 18, 23} {
		if s[i] != '-' {
			t.Fatalf("no hyphen at %d", i)
		}
	}
	var zero model.PersonID
	if zero.String() != "00000000-0000-0000-0000-000000000000" {
		t.Fatal(zero.String())
	}
}

func TestParseID_WrongLength_ReturnsInvalid(t *testing.T) {
	for _, in := range []string{"", "x", sample[:35], sample + "0", sample[:8]} {
		if _, err := model.ParseID[model.PersonID](in); !errs.Is(err, errs.Invalid) {
			t.Errorf("%q: %v", in, err)
		}
	}
}

func TestParseID_BadCharacterOrHyphen_ReturnsInvalid(t *testing.T) {
	tests := []string{
		"0190a1b2c3d4-4e5f-8a6b-7c8d9e0f1a2b0", // hyphen missing, right length
		"0190a1b2-c3d44-e5f-8a6b-7c8d9e0f1a2b", // misplaced hyphen
		"0190a1b2-c3d4-4e5f8-a6b-7c8d9e0f1a2b", // misplaced
		"0190a1b2-c3d4-4e5f-8a6b7-c8d9e0f1a2b", // misplaced
		"0190a1g2-c3d4-4e5f-8a6b-7c8d9e0f1a2b", // non-hex
		"0190a1b2-c3d4-4e5f-8a6b-7c8d9e0f1a2z", // non-hex last
		"zzzzzzzz-zzzz-zzzz-zzzz-zzzzzzzzzzzz", // all bad
		"0190a1b2_c3d4-4e5f-8a6b-7c8d9e0f1a2b", // underscore
		"+190a1b2-c3d4-4e5f-8a6b-7c8d9e0f1a2b", // sign
		"0190a1b2-c3d4-4e5f-8a6b-7c8d9e0f1a2é", // non-ascii (length may differ)
	}
	for _, in := range tests {
		if _, err := model.ParseID[model.PersonID](in); !errs.Is(err, errs.Invalid) {
			t.Errorf("%q: %v", in, err)
		}
	}
}

func TestParseID_NonCanonicalForms_ReturnInvalid(t *testing.T) {
	hex32 := strings.ReplaceAll(sample, "-", "")
	for _, in := range []string{
		"{" + sample + "}", "urn:uuid:" + sample, hex32,
		" " + sample, sample + " ", sample + "\n", "\t" + sample,
	} {
		if _, err := model.ParseID[model.PersonID](in); !errs.Is(err, errs.Invalid) {
			t.Errorf("%q: %v", in, err)
		}
	}
}

func TestIDUnmarshalText_Bad_ReturnsInvalidAndLeavesValue(t *testing.T) {
	orig, err := model.ParseID[model.PersonID](sample)
	if err != nil {
		t.Fatalf("ParseID(sample): %v", err)
	}
	for _, in := range []string{"", "nope", "{" + sample + "}", strings.ReplaceAll(sample, "-", "")} {
		id := orig
		if err := id.UnmarshalText([]byte(in)); !errs.Is(err, errs.Invalid) {
			t.Errorf("%q: %v", in, err)
		}
		if id != orig {
			t.Errorf("%q changed value", in)
		}
	}
}

func TestIDUnmarshalText_NilReceiver_ReturnsInternal(t *testing.T) {
	var p *model.PersonID
	if err := p.UnmarshalText([]byte(sample)); !errs.Is(err, errs.Internal) {
		t.Fatalf("got %v", err)
	}
}

func TestIDUnmarshalText_Good_Stores(t *testing.T) {
	var id model.PersonID
	if err := id.UnmarshalText([]byte(strings.ToUpper(sample))); err != nil {
		t.Fatal(err)
	}
	if id.String() != sample {
		t.Fatal(id)
	}
}

func TestIDValidate_Zero_ReturnsInvalid(t *testing.T) {
	var z model.PersonID
	err := z.Validate()
	if !errs.Is(err, errs.Invalid) {
		t.Fatalf("got %v", err)
	}
	if len(validate.Fields(err)) == 0 {
		t.Error("want a FieldError")
	}
}

func TestIDValidate_NonZeroAnyVersion_OK(t *testing.T) {
	var v7 model.PersonID
	v7[6], v7[8] = 0x70, 0x80
	if err := v7.Validate(); err != nil {
		t.Fatal(err)
	}
	var odd model.PersonID
	odd[15] = 1 // not even a valid version/variant
	if err := odd.Validate(); err != nil {
		t.Fatal(err)
	}
	if err := model.NewID[model.PersonID]().Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestIDIsZero_ZeroAndNonZero(t *testing.T) {
	var z model.EventID
	if !z.IsZero() {
		t.Error("zero should be zero")
	}
	var one model.EventID
	one[15] = 1
	if one.IsZero() {
		t.Error("non-zero reported zero")
	}
}

func TestIDMarshalText_NeverFails(t *testing.T) {
	id := model.NewID[model.PersonID]()
	b, err := id.MarshalText()
	if err != nil || string(b) != id.String() {
		t.Fatalf("%q %v", b, err)
	}
}

func TestIDJSON_RoundTripInStruct(t *testing.T) {
	type doc struct {
		Person  model.PersonID     `json:"person"`
		Event   model.EventID      `json:"event"`
		Opt     model.DepartmentID `json:"opt,omitzero"`
		Legacy  model.AccountID    `json:"legacy,omitempty"`
		Persons []model.PersonID   `json:"persons"`
	}
	in := doc{
		Person:  model.NewID[model.PersonID](),
		Event:   model.NewID[model.EventID](),
		Persons: []model.PersonID{model.NewID[model.PersonID](), model.NewID[model.PersonID]()},
	}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"person":"`+in.Person.String()+`"`) {
		t.Fatalf("json %s", b)
	}
	if strings.Contains(string(b), `"opt"`) {
		t.Errorf("omitzero should omit zero id: %s", b)
	}
	if !strings.Contains(string(b), `"legacy"`) {
		t.Errorf("omitempty must not omit a [16]byte id: %s", b)
	}
	var out doc
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	if out.Person != in.Person || out.Event != in.Event || len(out.Persons) != 2 || out.Persons[1] != in.Persons[1] {
		t.Fatalf("round trip %+v", out)
	}

	// null leaves the field unchanged.
	keep := doc{Person: in.Person}
	if err := json.Unmarshal([]byte(`{"person":null}`), &keep); err != nil {
		t.Fatal(err)
	}
	if keep.Person != in.Person {
		t.Error("null changed the field")
	}
	var fresh doc
	if err := json.Unmarshal([]byte(`{"person":null}`), &fresh); err != nil || !fresh.Person.IsZero() {
		t.Errorf("fresh: %v %v", fresh.Person, err)
	}

	// bad text fails.
	if err := json.Unmarshal([]byte(`{"person":"nope"}`), &fresh); err == nil {
		t.Error("want error for bad id text")
	}
}

func TestTypedIDs_ConvertToAndFromBytes(t *testing.T) {
	id := model.NewID[model.PersonID]()
	b := [16]byte(id)
	if model.PersonID(b) != id {
		t.Fatal("bytes round trip")
	}
	if model.DepartmentID(b).String() != id.String() {
		t.Fatal("same bytes, same text")
	}
}

func TestEntityAlias_ModuleStyleDeclaration(t *testing.T) {
	id := model.NewID[ShiftID]()
	if id.IsZero() || id.Validate() != nil {
		t.Fatal("new id invalid")
	}
	got, err := model.ParseID[ShiftID](id.String())
	if err != nil || got != id {
		t.Fatalf("parse: %v", err)
	}
	b, err := id.MarshalText()
	if err != nil {
		t.Fatal(err)
	}
	var back ShiftID
	if err := back.UnmarshalText(b); err != nil || back != id {
		t.Fatal("unmarshal", err)
	}
	var z ShiftID
	if !z.IsZero() || !errs.Is(z.Validate(), errs.Invalid) {
		t.Fatal("zero handling")
	}
}

func TestParseErrors_IDNeverEchoesInput(t *testing.T) {
	const marker = "SECRETMARKER-hunter2"
	_, err := model.ParseID[model.PersonID](marker)
	if err == nil {
		t.Fatal("want error")
	}
	assertNoEcho(t, err, marker)
	var id model.PersonID
	assertNoEcho(t, id.UnmarshalText([]byte(marker)), marker)
}

func assertNoEcho(t *testing.T, err error, marker string) {
	t.Helper()
	if err == nil {
		t.Fatal("want error")
	}
	if strings.Contains(err.Error(), marker) {
		t.Errorf("error text echoes input: %v", err)
	}
	for _, fe := range validate.Fields(err) {
		if strings.Contains(fe.Reason, marker) || strings.Contains(fe.Field, marker) {
			t.Errorf("FieldError echoes input: %+v", fe)
		}
		for k, v := range fe.Params {
			if strings.Contains(v, marker) || strings.Contains(k, marker) {
				t.Errorf("FieldError params echo input: %+v", fe)
			}
		}
	}
}
