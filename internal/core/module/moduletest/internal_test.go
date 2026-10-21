package moduletest

import (
	"strings"
	"testing"

	"github.com/zedgama3/bms/internal/core/audit"
	"github.com/zedgama3/bms/internal/core/jobs"
	"github.com/zedgama3/bms/internal/core/notify"
)

func TestAuditLeak_NamesAndIDsOnly_Passes(t *testing.T) {
	err := auditLeak([]audit.Entry{
		{Action: "hello.greeting.read", Fields: []string{"word", "count"}, Reason: "front desk lookup"},
		{Action: "hello.greeting.created"},
	})
	if err != nil {
		t.Fatalf("auditLeak = %v, want nil", err)
	}
}

func TestAuditLeak_Table(t *testing.T) {
	for _, tc := range []struct {
		name  string
		entry audit.Entry
	}{
		{"legal name in reason", audit.Entry{Action: "hello.greeting.read", Reason: "asked about " + subject.LegalName}},
		{"phone in a field name", audit.Entry{Action: "hello.greeting.read", Fields: []string{subject.Phone}}},
		{"scrubbed e-mail in reason", audit.Entry{Action: "hello.greeting.read", Reason: "wrote to " + scrubbedEmail}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := auditLeak([]audit.Entry{tc.entry}); err == nil {
				t.Fatal("auditLeak accepted a marker")
			}
		})
	}
}

func TestSentLeak_Table(t *testing.T) {
	msg := func(vars map[string]string, link string) []notify.Message {
		return []notify.Message{{Template: "alpha.greeted", Vars: vars, Link: link}}
	}
	for _, tc := range []struct {
		name    string
		msgs    []notify.Message
		wantErr bool
	}{
		{"nothing sent", nil, false},
		{"plain vars and link", msg(map[string]string{"word": "hello"}, "/alpha/"), false},
		{"legal name in a var", msg(map[string]string{"who": subject.LegalName}, ""), true},
		{"phone in a var", msg(map[string]string{"p": subject.Phone}, ""), true},
		{"note in the link", msg(nil, "/alpha/"+subject.Note), true},
		{"scrubbed e-mail in a var", msg(map[string]string{"to": scrubbedEmail}, ""), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := sentLeak(tc.msgs); (err != nil) != tc.wantErr {
				t.Fatalf("sentLeak = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}

func TestKeyPersonLeak_Table(t *testing.T) {
	id := subject.Person.String()
	for _, tc := range []struct {
		name string
		key  string
		bad  bool
	}{
		{"no key", "", false},
		{"a record's own ID", "shift:0b1c2d3e", false},
		{"the person's ID", "person:" + id, true},
		{"the person's ID, upper case", "p-" + strings.ToUpper(id), true},
		{"the person's ID without dashes", "p." + strings.ReplaceAll(id, "-", ""), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := keyPersonLeak([]jobs.Request{{Name: "hello.remind", Key: tc.key}})
			if (err != nil) != tc.bad {
				t.Fatalf("keyPersonLeak(%q) = %v, want error %v", tc.key, err, tc.bad)
			}
		})
	}
}
