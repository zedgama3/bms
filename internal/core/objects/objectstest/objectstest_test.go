package objectstest_test

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"testing"

	"github.com/zedgama3/bms/internal/core/objects"
	"github.com/zedgama3/bms/internal/core/objects/objectstest"
)

func TestMem_Conformance(t *testing.T) {
	objectstest.Conformance(t, func(*testing.T) objects.Store { return objectstest.NewMem() })
}

func TestMem_InstancesAreIndependent(t *testing.T) {
	a, b := objectstest.NewMem(), objectstest.NewMem()
	data := []byte("x")
	_, err := a.Put(t.Context(), objects.PutInput{Bucket: "files", Key: "k", Size: 1, SHA256: sha256.Sum256(data), Body: bytes.NewReader(data)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b.Head(t.Context(), "files", "k"); !errors.Is(err, objects.ErrMissing) {
		t.Errorf("a second NewMem sees the first one's object: %v", err)
	}
}
