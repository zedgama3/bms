package jobs_test

import (
	"errors"

	"github.com/zedgama3/bms/internal/core/db"
)

func isReadOnly(err error) bool { return errors.Is(err, db.ErrReadOnly) }
