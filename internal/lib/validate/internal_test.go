package validate

import "testing"

type secretString string

func TestInternalRangeStringKindDoesNotEcho(t *testing.T) {
	for _, err := range []error{
		Range("f", "hunter2", "a", "b"),
		Range("f", secretString("hunter2"), "a", "b"),
	} {
		fs := Fields(err)
		if len(fs) != 1 || fs[0].Reason != "must be between a and b" {
			t.Errorf("got %v", fs)
		}
	}
}

func TestInternalURLEmptyHostname(t *testing.T) {
	if URL("u", "http://:8080/hook") == nil {
		t.Error("empty hostname accepted")
	}
	if err := URL("u", "http://h:8080/hook", "http"); err != nil {
		t.Error(err)
	}
}
