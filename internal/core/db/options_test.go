package db_test

import (
	"strings"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/secret"
	"github.com/zedgama3/bms/internal/lib/validate"
)

func fieldNames(err error) map[string]bool {
	out := map[string]bool{}
	for _, f := range validate.Fields(err) {
		out[f.Field] = true
	}
	return out
}

func TestOptionsValidate_Valid_ReturnsNil(t *testing.T) {
	if err := validOptions().Validate(); err != nil {
		t.Fatalf("valid options: %v", err)
	}
	// Tracer and Meter are optional: nil is a valid value.
	o := validOptions()
	o.Tracer, o.Meter = nil, nil
	if err := o.Validate(); err != nil {
		t.Fatalf("nil tracer and meter: %v", err)
	}
}

// F1
func TestOptionsValidate_URLUnset_ReturnsInvalid(t *testing.T) {
	for name, u := range map[string]secret.Secret{
		"zero value":   {},
		"empty string": secret.New(""),
	} {
		t.Run(name, func(t *testing.T) {
			o := validOptions()
			o.URL = u
			err := o.Validate()
			wantKind(t, err, errs.Invalid)
			if !fieldNames(err)["db.url"] {
				t.Fatalf("want a db.url field error, got %v", fieldNames(err))
			}
		})
	}
}

// F2
func TestOptionsValidate_BadURL_ReturnsInvalidWithoutEcho(t *testing.T) {
	for name, u := range map[string]string{
		"wrong scheme":         "mysql://bms:" + marker + "@localhost/bms",
		"no scheme":            "bms:" + marker + "@localhost/bms",
		"relative":             "/var/run/" + marker,
		"no host":              "postgres://bms:" + marker + "@/bms",
		"empty host with path": "postgres:///" + marker,
		"garbage":              "not a url " + marker,
		"http":                 "http://localhost/" + marker,
	} {
		t.Run(name, func(t *testing.T) {
			o := validOptions()
			o.URL = secret.New(u)
			err := o.Validate()
			wantKind(t, err, errs.Invalid)
			if !fieldNames(err)["db.url"] {
				t.Fatalf("want a db.url field error, got %v", fieldNames(err))
			}
			noMarker(t, err)
			for _, f := range validate.Fields(err) {
				if strings.Contains(f.Reason, marker) {
					t.Fatalf("field reason echoes the URL")
				}
			}
		})
	}
	for _, scheme := range []string{"postgres", "postgresql"} {
		o := validOptions()
		o.URL = secret.New(scheme + "://bms:" + marker + "@db.example.org:5432/bms")
		if err := o.Validate(); err != nil {
			t.Errorf("%s scheme must be accepted: %v", scheme, err)
		}
	}
}

// F3 and TestOptionsValidate_MirrorsConfigBounds: the bounds of config.md's catalogue.
func TestOptionsValidate_RangesTable(t *testing.T) {
	tests := []struct {
		name  string
		set   func(*db.Options)
		field string
		ok    bool
	}{
		{"max_conns 0", func(o *db.Options) { o.MaxConns = 0 }, "db.max_conns", false},
		{"max_conns -1", func(o *db.Options) { o.MaxConns = -1 }, "db.max_conns", false},
		{"max_conns 1", func(o *db.Options) { o.MaxConns = 1 }, "db.max_conns", true},
		{"max_conns 1000", func(o *db.Options) { o.MaxConns = 1000 }, "db.max_conns", true},
		{"max_conns 1001", func(o *db.Options) { o.MaxConns = 1001 }, "db.max_conns", false},
		{"connect_timeout zero", func(o *db.Options) { o.ConnectTimeout = 0 }, "db.connect_timeout", false},
		{"connect_timeout below", func(o *db.Options) { o.ConnectTimeout = time.Second - 1 }, "db.connect_timeout", false},
		{"connect_timeout min", func(o *db.Options) { o.ConnectTimeout = time.Second }, "db.connect_timeout", true},
		{"connect_timeout max", func(o *db.Options) { o.ConnectTimeout = 10 * time.Minute }, "db.connect_timeout", true},
		{"connect_timeout above", func(o *db.Options) { o.ConnectTimeout = 10*time.Minute + 1 }, "db.connect_timeout", false},
		{"query_timeout zero", func(o *db.Options) { o.QueryTimeout = 0 }, "db.query_timeout", false},
		{"query_timeout below", func(o *db.Options) { o.QueryTimeout = 100*time.Millisecond - 1 }, "db.query_timeout", false},
		{"query_timeout min", func(o *db.Options) { o.QueryTimeout = 100 * time.Millisecond }, "db.query_timeout", true},
		{"query_timeout max", func(o *db.Options) { o.QueryTimeout = 10 * time.Minute }, "db.query_timeout", true},
		{"query_timeout above", func(o *db.Options) { o.QueryTimeout = 10*time.Minute + 1 }, "db.query_timeout", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			o := validOptions()
			tc.set(&o)
			err := o.Validate()
			if tc.ok {
				if err != nil {
					t.Fatalf("want ok, got %v", err)
				}
				return
			}
			wantKind(t, err, errs.Invalid)
			if !fieldNames(err)[tc.field] {
				t.Fatalf("want field %s, got %v", tc.field, fieldNames(err))
			}
		})
	}

	t.Run("all problems reported at once", func(t *testing.T) {
		err := db.Options{}.Validate()
		wantKind(t, err, errs.Invalid)
		got := fieldNames(err)
		for _, f := range []string{"db.url", "db.max_conns", "db.connect_timeout", "db.query_timeout"} {
			if !got[f] {
				t.Errorf("missing field error %s in %v", f, got)
			}
		}
		if n := len(validate.Fields(err)); n != 4 {
			t.Errorf("want one FieldError per problem (4), got %d", n)
		}
	})
}

func TestMinServerVersion_IsPostgres16(t *testing.T) {
	if db.MinServerVersion != 160000 {
		t.Fatalf("MinServerVersion = %d, want 160000", db.MinServerVersion)
	}
}
