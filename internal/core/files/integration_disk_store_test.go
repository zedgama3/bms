//go:build integration

package files_test

import (
	"testing"

	"github.com/zedgama3/bms/internal/core/objects"
	"github.com/zedgama3/bms/internal/core/objects/disk" //nolint:depguard // D52(e): the lifecycle tests also run over the real disk store
)

// newDiskStore is a disk object store rooted in a fresh t.TempDir().
func newDiskStore(t *testing.T) objects.Store {
	t.Helper()
	st, err := disk.New(t.TempDir())
	if err != nil {
		t.Fatalf("disk.New: %v", err)
	}
	return st
}
