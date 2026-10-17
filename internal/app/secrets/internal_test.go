package secrets

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/zedgama3/bms/internal/lib/errs"
)

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, errors.New("entropy source is gone") }

// F26: a failing random source is reported as Internal and writes nothing.
func TestEnsure_RandFails_ReturnsInternal(t *testing.T) {
	old := randReader
	randReader = failingReader{}
	t.Cleanup(func() { randReader = old })

	path := filepath.Join(t.TempDir(), "data", "secrets.json")
	created, err := Ensure(context.Background(), path, []string{"session_key"})
	if !errs.Is(err, errs.Internal) {
		t.Fatalf("err = %v, want kind Internal", err)
	}
	if created != nil {
		t.Errorf("created = %v, want nil", created)
	}
	if _, serr := os.Stat(path); !errors.Is(serr, os.ErrNotExist) {
		t.Errorf("a failed Ensure left %s behind (stat error: %v)", path, serr)
	}
}
