package account

import (
	"time"

	"github.com/zedgama3/bms/internal/model"
)

type reset struct{}

// ResetID identifies one row of core.password_reset. It is the idempotency key of the
// reset e-mail ("password-reset:" + the ID) and is not secret.
type ResetID = model.ID[reset]

// ResetRow is a password-reset token as the store reports it. The token itself is never
// stored: the row is found by the SHA-256 of the token, and the hash is not part of the
// row the service sees.
type ResetRow struct {
	ID              ResetID
	Account         model.AccountID
	ExpiresAt       time.Time
	UsedAt          time.Time // zero while unused
	CreatedAt       time.Time
	AccountDisabled bool
}
