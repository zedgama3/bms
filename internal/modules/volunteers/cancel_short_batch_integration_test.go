//go:build integration

package volunteers_test

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5" //nolint:depguard // wrapping db.DB.Query needs its return type

	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/departments"
	"github.com/zedgama3/bms/internal/core/signals"
	"github.com/zedgama3/bms/internal/lib/errs"
)

// racingCancelDB makes the first cancel-shifts batch statement come back short, for real: a
// second transaction locks the earliest shift of the department, the batch UPDATE blocks on
// that row, and the second transaction cancels the shift and commits. Under READ COMMITTED
// the UPDATE then re-checks canceled_at IS NULL and skips the row, so the batch returns
// fewer rows than its limit even though more future shifts remain.
type racingCancelDB struct {
	db.DB
	pool  *db.Pool
	t     *testing.T
	armed atomic.Bool
	fired atomic.Bool
	done  chan error
}

func (c *racingCancelDB) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) { //nolint:contextcheck // the second transaction must not share the first one's
	if !strings.Contains(sql, "UPDATE volunteers.shift") || !strings.Contains(sql, "RETURNING id") ||
		!c.armed.Load() || !c.fired.CompareAndSwap(false, true) {
		return c.DB.Query(ctx, sql, args...) //nolint:wrapcheck // test wrapper
	}
	other := ctxT(c.t) // no transaction: a different connection
	locked := make(chan struct{})
	c.done = make(chan error, 1)
	go func() {
		c.done <- c.pool.Transact(other, db.Write, func(tx context.Context) error {
			var id [16]byte
			if err := c.pool.QueryRow(tx, `SELECT id FROM volunteers.shift WHERE department_id = $1::uuid AND canceled_at IS NULL
				ORDER BY starts, id LIMIT 1 FOR UPDATE`, args[0]).Scan(&id); err != nil {
				close(locked)
				return err //nolint:wrapcheck // test
			}
			close(locked)
			tick := time.NewTicker(2 * time.Millisecond)
			defer tick.Stop()
			for {
				var n int
				if err := c.pool.QueryRow(other, `SELECT count(*) FROM pg_stat_activity
					WHERE wait_event_type = 'Lock' AND query LIKE '%Store.CancelShiftsByDepartment%'`).Scan(&n); err != nil {
					return err //nolint:wrapcheck // test
				}
				if n > 0 {
					break
				}
				select {
				case <-other.Done():
					return other.Err()
				case <-tick.C:
				}
			}
			_, err := c.pool.Exec(tx, `UPDATE volunteers.shift SET canceled_at = now(), cancel_reason = 'lead', updated_at = now() WHERE id = $1::uuid`, id)
			return err //nolint:wrapcheck // test
		})
	}()
	<-locked
	return c.DB.Query(ctx, sql, args...) //nolint:wrapcheck // test wrapper
}

// S1 regression, decisions 12 and 23: a batch that comes back short because one of its
// shifts was canceled concurrently must not end the job; it runs until a batch returns 0.
func TestCancelShiftsJob_ShortBatchFromConcurrentCancel_KeepsGoing(t *testing.T) {
	var racing *racingCancelDB
	r, d, n := newBatchWorld(t, func(o *opts) {
		pool, ok := o.db.(*db.Pool)
		if !ok {
			t.Fatal("the rig database is not a *db.Pool")
		}
		racing = &racingCancelDB{DB: o.db, pool: pool, t: t}
		o.db = racing
	})
	r.archiveDepartment(d)
	wantNoErr(t, deliver(r, departments.SignalArchived, signals.Ref("department", d)))
	racing.armed.Store(true)
	wantNoErr(t, r.drainCancelJobs())
	if !racing.fired.Load() {
		t.Fatal("the racing cancel never ran; the fixture is wrong")
	}
	if err := <-racing.done; err != nil && !errors.Is(err, context.Canceled) {
		t.Fatalf("the concurrent cancel: %v (%v)", err, errs.KindOf(err))
	}
	if got := canceledShifts(t, r, d); got != n {
		t.Errorf("%d of %d shifts canceled; a short batch ended the job early", got, n)
	}
	var booked int
	if err := r.db.QueryRow(ctxT(t), `SELECT count(*) FROM volunteers.signup su JOIN volunteers.shift s ON s.id = su.shift_id
		WHERE s.department_id = $1::uuid AND su.status = 'booked'`, [16]byte(d)).Scan(&booked); err != nil {
		t.Fatalf("count: %v", err)
	}
	if booked != 1 {
		t.Errorf("%d booked sign-ups remain, want only the concurrently canceled shift's", booked)
	}
}
