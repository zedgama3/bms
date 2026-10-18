// Package audittest gives tests an audit.Recorder that needs no database and no files.
package audittest

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"sync"

	"github.com/zedgama3/bms/internal/core/audit"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/validate"
)

// areaRE is the grammar of a recorder's area, as audit.Log.Scoped takes it.
var areaRE = regexp.MustCompile(`^[a-z][a-z0-9]{1,31}$`)

// checkBatch repeats the real recorder's checks through the exported API: the batch size,
// Entry.Validate and the area, reporting field errors as "entries" and
// "entries[i].<field>". TestFake_MatchesRealValidation keeps the two in step.
func checkBatch(area string, entries []audit.Entry) error {
	const op = "audittest.Fake.Record"
	if len(entries) == 0 || len(entries) > audit.MaxBatch {
		v := validate.Errors{Op: op}
		v.Addf("entries", validate.CodeRange, "must hold 1 to %d entries", audit.MaxBatch)
		return v.Err() //nolint:wrapcheck // Err already carries Op and errs.Invalid
	}
	v := validate.Errors{Op: op}
	for i, e := range entries {
		prefix := "entries[" + strconv.Itoa(i) + "]."
		for _, fe := range validate.Fields(e.Validate()) {
			v.Add(errs.E("", errs.Invalid, &validate.FieldError{Field: prefix + fe.Field, Code: fe.Code, Params: fe.Params, Reason: fe.Reason}))
		}
		if e.Action.Validate() == nil && e.Action.Area() != area {
			v.Addf(prefix+"action", "mismatch", "must belong to the recorder's area")
		}
	}
	return v.Err() //nolint:wrapcheck // Err already carries Op and errs.Invalid
}

// Fake is a Recorder for tests: it validates entries like the real one (grammar, area,
// limits), keeps them in memory, and never touches a database or a file. Safe for
// concurrent use.
type Fake struct {
	area string

	mu       sync.Mutex
	recorded []audit.Entry
	err      error
}

var _ audit.Recorder = (*Fake)(nil)

// NewFake returns a Fake scoped to area. It panics when area is not a valid area (a
// mistake in test setup, caught at once).
func NewFake(area string) *Fake {
	if !areaRE.MatchString(area) {
		panic(fmt.Sprintf("audittest.NewFake: invalid area %q", area))
	}
	return &Fake{area: area}
}

// Record validates every entry exactly as the real recorder does (Entry.Validate plus the area check, with the same field names); on
// success it appends them in order. After Fail(err) it returns err instead, recording
// nothing. Fails errs.Invalid for a bad batch.
func (f *Fake) Record(ctx context.Context, entries ...audit.Entry) error {
	if err := ctx.Err(); err != nil {
		return errs.E("audittest.Fake.Record", errs.KindOf(err), err)
	}
	if err := checkBatch(f.area, entries); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	for _, e := range entries {
		e.Fields = append([]string(nil), e.Fields...)
		f.recorded = append(f.recorded, e)
	}
	return nil
}

// Recorded returns every entry recorded so far, in call order, as a copy.
func (f *Fake) Recorded() []audit.Entry {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]audit.Entry, len(f.recorded))
	for i, e := range f.recorded {
		e.Fields = append([]string(nil), e.Fields...)
		out[i] = e
	}
	return out
}

// Fail makes the next Record calls fail with err, after validating; nil clears it.
func (f *Fake) Fail(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.err = err
}
