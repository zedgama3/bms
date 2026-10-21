//go:build integration

package objects_test

import (
	"errors"

	"github.com/zedgama3/bms/internal/core/objects"
)

func isErrPublic(err error) bool { return errors.Is(err, objects.ErrPublic) }
