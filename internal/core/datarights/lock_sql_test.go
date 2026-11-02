package datarights_test

import (
	"bytes"
	"os"
	"testing"
)

// The deletion lock key is shared: datarights' Delete and people's UpdateMe and LinkAccount
// must take the same advisory lock, so the two statements must not drift apart. Comments are not compared.
func stripComments(b []byte) []byte {
	var out [][]byte
	for _, l := range bytes.Split(b, []byte("\n")) {
		if !bytes.HasPrefix(bytes.TrimSpace(l), []byte("--")) {
			out = append(out, l)
		}
	}
	return bytes.Join(out, []byte("\n"))
}

func TestLockPersonDeletion_SQLIsByteIdenticalToPeoples(t *testing.T) {
	mine, err := os.ReadFile("postgres/sql/lock_person_deletion.sql")
	if err != nil {
		t.Fatalf("read datarights' statement: %v", err)
	}
	theirs, err := os.ReadFile("../people/postgres/sql/lock_person_deletion.sql")
	if err != nil {
		t.Fatalf("read people's statement: %v", err)
	}
	if len(mine) == 0 {
		t.Fatal("datarights' lock statement is empty")
	}
	// The leading comments name each other as twins, so they differ by design; the statement
	// itself (every non-comment line) must be byte-identical.
	mine, theirs = stripComments(mine), stripComments(theirs)
	if len(mine) == 0 || !bytes.Equal(mine, theirs) {
		t.Errorf("lock_person_deletion.sql differs between datarights (%d bytes) and people (%d bytes): the two must take one lock", len(mine), len(theirs))
	}
}
