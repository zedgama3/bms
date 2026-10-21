package objectstest

import (
	"testing"

	"github.com/zedgama3/bms/internal/core/objects"
)

// TestMem_PassesConformance runs the shared contract suite over Mem.
func TestMem_PassesConformance(t *testing.T) {
	Conformance(t, func(*testing.T) objects.Store { return NewMem() })
}
