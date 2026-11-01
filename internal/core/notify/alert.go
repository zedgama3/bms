package notify

import (
	"context"
	"errors"
	"fmt"

	"github.com/zedgama3/bms/internal/lib/errs"
)

// ErrNoAlertAddress marks "notify.alert_addresses is empty" (errs.Unsupported). It is not
// ErrNotConfigured, which marks SMTP that is not set up.
var ErrNoAlertAddress = errors.New("no notify.alert_addresses entry is configured")

// alerts is an AlertSender bound to one owner.
type alerts struct {
	s     *Service
	owner string
}

var _ AlertSender = alerts{}

// Alerts returns the AlertSender of owner (scoped like Scoped: it sends only that owner's
// templates). Fails errs.Invalid for a malformed owner and errs.Internal after Run started.
func (s *Service) Alerts(owner string) (AlertSender, error) {
	const op = "notify.Service.Alerts"
	if s.running.Load() {
		return nil, errs.E(op, errs.Internal, errors.New("Alerts after Run"))
	}
	if err := checkOwner(op, owner); err != nil {
		return nil, err
	}
	return alerts{s: s, owner: owner}, nil
}

// SendAlert implements AlertSender: m goes to every notify.alert_addresses entry, one row
// per address in the site's default language, with the owner's template. SendAlert reads
// only notify.alert_addresses; each enqueue checks Configured as Send does. A failure for one address does not stop the others; the
// failures are joined under the kind of the first. Fails errs.Invalid for a template of
// another owner (checked first: a programming error), errs.Unsupported wrapping
// ErrNoAlertAddress when no address is configured (checked before SMTP), the settings
// Reader's kind when the addresses cannot be read, and otherwise as Send (errs.Unsupported
// wrapping ErrNotConfigured, errs.Unavailable wrapping db.ErrReadOnly on a read-only copy,
// store kinds). Errors never carry an address.
func (a alerts) SendAlert(ctx context.Context, m Message) error {
	const op = "notify.Alerts.SendAlert"
	if m.Template.Owner() != a.owner {
		return fieldError(op, "template", codeScope, nil, "the template belongs to another owner")
	}
	v, err := a.s.settings.Text(ctx, "alert_addresses")
	if err != nil {
		return seamError(op, "the alert addresses", err)
	}
	addrs, err := ParseAddresses(v)
	if err != nil {
		return errs.E(op, errs.Invalid, errors.New("notify.alert_addresses does not parse"))
	}
	if len(addrs) == 0 {
		return errs.E(op, errs.Unsupported, ErrNoAlertAddress)
	}
	var failed []error
	for _, to := range addrs {
		if _, err := a.s.enqueue(ctx, &a.owner, to, m); err != nil {
			failed = append(failed, fmt.Errorf("an alert address: %w", err))
		}
	}
	if len(failed) == 0 {
		return nil
	}
	return errs.E(op, errs.KindOf(failed[0]), errors.Join(failed...))
}
