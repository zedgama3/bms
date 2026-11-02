package app

import (
	"testing"

	"github.com/zedgama3/bms/internal/lib/errs"
)

func TestNewSchemaStatus_NilMigrator_ReturnsInternal(t *testing.T) {
	if _, err := newSchemaStatus(nil, nil); !errs.Is(err, errs.Internal) {
		t.Errorf("err = %v, want Internal", err)
	}
}
