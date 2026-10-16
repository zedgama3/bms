package secret_test

import (
	"fmt"

	"github.com/zedgama3/bms/internal/lib/secret"
)

func ExampleNew() {
	s := secret.New("hunter2")
	fmt.Println(s)
	fmt.Println(s.Value()) // the one legitimate read
	fmt.Println(secret.New(""))
	// Output:
	// [redacted]
	// hunter2
	// [unset]
}

func ExampleSecret_Format() {
	s := secret.New("hunter2")
	fmt.Printf("%v|%-12v|%12s|%q|%.3s\n", s, s, s, s, s)
	// Output:
	// [redacted]|[redacted]  |  [redacted]|"[redacted]"|[re
}
