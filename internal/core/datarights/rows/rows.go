// Package rows holds the row type datarights' service and its PostgreSQL store share. It is
// a leaf: it imports model only, because datarights imports postgres to build its store and
// so postgres cannot import datarights. Statuses and kinds travel as the words the table
// holds.
package rows

import (
	"time"

	"github.com/zedgama3/bms/internal/model"
)

type request struct{}

// RequestID identifies a deletion request. datarights re-exports it under the same name.
type RequestID = model.ID[request]

// The request statuses, as core.data_request.status holds them.
const (
	StatusOpen      = "open"
	StatusWithdrawn = "withdrawn"
	StatusDeclined  = "declined"
)

// KindDelete is the only core.data_request.kind in M2.
const KindDelete = "delete"

// Request is a row of core.data_request. A zero DecidedAt and DecidedBy are NULL.
type Request struct {
	ID           RequestID
	Person       model.PersonID
	Kind         string
	Status       string
	Note         string
	CreatedAt    time.Time
	DecidedAt    time.Time
	DecidedBy    model.PersonID
	DecisionNote string
}
