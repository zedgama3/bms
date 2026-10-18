package moduletest

import (
	"testing"

	"github.com/zedgama3/bms/internal/core/audit"
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
