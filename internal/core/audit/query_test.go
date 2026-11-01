package audit_test

import (
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/core/audit"
	"github.com/zedgama3/bms/internal/model"
)

func TestQuery_Validate_Valid(t *testing.T) {
	for name, q := range map[string]audit.Query{
		"zero":             {},
		"limit max":        {Limit: audit.MaxPageSize},
		"limit one":        {Limit: 1},
		"all filters":      {Actor: alice, Subject: bob, Record: audit.RefOf("identity.account", model.AccountID{1}), Area: "identity", Action: "identity.login", Within: model.TimeRange{Start: epoch, End: epoch.Add(time.Hour)}, SuperuserOnly: true},
		"area with digits": {Area: "area1"},
	} {
		t.Run(name, func(t *testing.T) { wantNoErr(t, q.Validate()) })
	}
}
