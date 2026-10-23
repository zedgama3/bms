//go:build integration

package volunteers_test

import (
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/modules/volunteers/vol"
)

// More shifts than one batch (vol.MaxList) are all copied, with the same start times across
// the batch boundary, and the true count is reported; a second run copies nothing.
func TestCopyShifts_MoreThanOneBatch_CopiesAll(t *testing.T) {
	w := newCopyWorld(t)
	const total = vol.MaxList + 2
	for i := range total {
		// Five shifts share each start time so a batch boundary can fall between equals.
		w.r.shift(w.d, w.pos.ID, w.from, w.fromStart.Add(time.Duration(i/5)*time.Hour), 1, 1)
	}
	n, err := w.r.svc.CopyShifts(w.r.as(w.r.boss), w.d, w.from, w.to)
	wantNoErr(t, err)
	if n != total {
		t.Fatalf("copied %d shifts, want %d", n, total)
	}
	n, err = w.r.svc.CopyShifts(w.r.as(w.r.boss), w.d, w.from, w.to)
	wantNoErr(t, err)
	if n != 0 {
		t.Errorf("second copy = %d, want 0", n)
	}
}
