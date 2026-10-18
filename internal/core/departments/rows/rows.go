// Package rows holds the row types departments' service and its PostgreSQL store share. It
// is a leaf: it imports model only, because departments imports postgres to build its store
// and so postgres cannot import departments.
package rows

import (
	"time"

	"github.com/zedgama3/bms/internal/model"
)

// Department is a row of core.department. ContactEmail "" is none. A zero ArchivedAt is
// NULL: the department is active.
type Department struct {
	ID           model.DepartmentID
	Name         string
	Description  string
	ContactEmail string
	ArchivedAt   time.Time
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// Patch changes the columns whose field is non-nil.
type Patch struct {
	ID           model.DepartmentID
	Name         *string
	Description  *string
	ContactEmail *string
}
