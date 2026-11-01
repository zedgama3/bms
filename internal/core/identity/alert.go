package identity

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/zedgama3/bms/internal/core/notify"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
)

// The superuser alert (D23, F27; app.md "The superuser alert").
const (
	// templateSuperuserSignedIn is the alert's template (Templates).
	templateSuperuserSignedIn notify.Template = "identity.superuser-signed-in"
	// alertLink is where the alert points: the audit log, filtered to the superuser's entries.
	alertLink = "/audit?superuser=1"
	// alertTimeout bounds one alert, so a slow database cannot hold a sign-in for long.
	alertTimeout = 5 * time.Second
)

// alert tells the holders of accounts.manage (Notify.Send) and each of the
// notify.alert_addresses (Alerts.SendAlert) that the superuser signed in. It runs under its
// own deadline (alertTimeout) that a disconnecting client cannot cancel, and sends with an
// empty Key: every sign-in is its own event, and a key would silently drop a later alert (at
// sign-in ctx can carry an old session from the request's cookie, so a session-based key
// would let a reused cookie suppress later alerts). A failure is logged (person ID and error
// kind only, never an address) and counted and never fails the sign-in (F27): the audit
// entry is the record.
func (s *Service) alert(ctx context.Context, person model.PersonID) {
	actx, cancel := context.WithTimeout(context.WithoutCancel(ctx), alertTimeout)
	defer cancel()
	if err := s.sendAlert(actx); err != nil {
		s.metrics.alertFailure(ctx)
		s.logger.WarnContext(ctx, "could not alert about a superuser sign-in",
			slog.String("person_id", person.String()), slog.String("error.kind", errs.KindOf(err).String()))
	}
}

// sendAlert tells every recipient it can and returns what failed, joined under the kind of
// the first failure: errs.Unsupported when e-mail is not configured or there is nobody to
// tell, errs.Unavailable wrapping db.ErrReadOnly on a read-only copy, and the kinds of
// authz, settings and notify. A failure to find the holders or to reach the alert addresses
// does not stop the other group.
func (s *Service) sendAlert(ctx context.Context) error {
	const op = "identity.Service.alert"
	var failed []error
	holders, err := s.holders.HoldersOf(ctx, PermissionAccountsManage)
	if err != nil {
		failed = append(failed, fmt.Errorf("holders: %w", err))
	}
	m := notify.Message{Template: templateSuperuserSignedIn, Link: alertLink}
	if len(holders) > 0 {
		hm := m
		hm.To = holders
		if err := s.notify.Send(ctx, hm); err != nil {
			failed = append(failed, fmt.Errorf("send to holders: %w", err))
		}
	}
	noAddress := false
	switch err := s.alerts.SendAlert(ctx, m); {
	case err == nil:
	case errors.Is(err, notify.ErrNoAlertAddress):
		noAddress = true // no alert address is configured: not a failure while someone else is told
	default:
		failed = append(failed, fmt.Errorf("alert addresses: %w", err))
	}
	if len(holders) == 0 && noAddress && len(failed) == 0 {
		return errs.E(op, errs.Unsupported, errors.New("no holder of accounts.manage and no alert address: nobody to tell"))
	}
	if len(failed) == 0 {
		return nil
	}
	return errs.E(op, errs.KindOf(failed[0]), errors.Join(failed...))
}
