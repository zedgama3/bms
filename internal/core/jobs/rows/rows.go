// Package rows holds the record types jobs' service and its PostgreSQL store share. It is a
// leaf: it imports model only, because jobs imports postgres to build its store and so
// postgres cannot import the root. Statuses travel as the words core.job holds
// ("queued", "running", "done", "failed", "canceled") and a zero time means NULL.
package rows

import (
	"time"

	"github.com/zedgama3/bms/internal/model"
)

type job struct{}

// ID identifies one row of core.job. jobs re-exports it as JobID.
type ID = model.ID[job]

// The statuses, as core.job.status holds them.
const (
	Queued   = "queued"
	Running  = "running"
	Done     = "done"
	Failed   = "failed"
	Canceled = "canceled"
)

// New is a deferred job to insert: what Enqueue and Defer hand the store. A zero Key stores
// NULL, a zero TraceID stores NULL.
type New struct {
	ID      ID
	Name    string
	Key     string
	Args    []byte // the JSON object
	RunAt   time.Time
	TraceID string
}

// Lease is a row a worker has just leased. LeasedUntil is the fence: pass it back unchanged
// to every later write of the run (the heartbeat returns the next one).
type Lease struct {
	ID          ID
	Name        string
	Key         string // "" when the row has no key
	Args        []byte // the JSON object
	Attempts    int    // already counting this lease
	RunAt       time.Time
	LeasedUntil time.Time
	TraceID     string // "" when none
	StartedAt   time.Time
}

// Reclaimed is a row a sweep settled: a Running row whose lease had lapsed. Status is Queued
// (back in the queue, last_error lost_lease) or Failed (last_error gave_up: its attempts reached
// the limit). Attempts is the count the row holds.
type Reclaimed struct {
	ID       ID
	Name     string
	Status   string
	Attempts int
}

// Job is one row as the status screen sees it. Zero times and empty strings stand for NULL.
type Job struct {
	ID         ID
	Name       string
	Key        string
	Status     string
	Attempts   int
	RunAt      time.Time
	StartedAt  time.Time
	FinishedAt time.Time
	LastError  string
	Instance   string
	TraceID    string
}

// QueueStat is the depth of one open status, for the bms.jobs.queue gauge.
type QueueStat struct {
	Status    string // Queued or Running
	Depth     int    // rows in that status
	Due       int    // rows with run_at <= now
	OldestDue time.Time
}
