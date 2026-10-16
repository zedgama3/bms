package validate_test

import (
	"fmt"
	"time"

	"github.com/zedgama3/bms/internal/lib/validate"
)

func ExampleErrors() {
	var v validate.Errors
	v.Op = "config.Validate"
	v.Add(validate.NonEmpty("db.url", " "))
	v.Add(validate.Positive("jobs.interval", -time.Second))
	v.Add(validate.HostPort("smtp.host", "mail1:587"))
	for _, f := range validate.Fields(v.Err()) {
		fmt.Println(f)
	}
	// Output:
	// db.url: must not be empty
	// jobs.interval: must be positive, got -1s
}
