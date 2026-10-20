package app

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/zedgama3/bms/internal/core/authz"
	"github.com/zedgama3/bms/internal/core/identity"
	"github.com/zedgama3/bms/internal/core/notify"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
)

// templateSuperuserSignedIn is identity's alert template (identity.Templates).
const templateSuperuserSignedIn notify.Template = "identity.superuser-signed-in"

// alertLink is where the alert points: the audit log, filtered to the superuser's entries.
const alertLink = "/audit?superuser=1"

// alertTimeout bounds one alert, so a slow database cannot hold a sign-in for long.
const alertTimeout = 5 * time.Second

// lazyAlerter is identity's Alerter. It resolves the notify service, authz and settings,
// which are built after identity, when called (app.md, "The superuser alert"). It does no
// I/O of its own beyond what notify, authz and settings do.
type lazyAlerter struct{ a *assembly }

var _ identity.Alerter = lazyAlerter{}

// SuperuserSignedIn e-mails the holders of accounts.manage and each of the
// notify.alert_addresses that the superuser signed in. It runs under its own deadline
// (alertTimeout) that a disconnecting client cannot cancel, reads only the alert addresses
// from the settings, and sends with an empty Key: every sign-in is its own event, and a key
// would silently drop a later alert. See alertSeams.send for the failures. identity logs and
// counts a failure and never fails the sign-in; the audit entry remains the record.
func (l lazyAlerter) SuperuserSignedIn(ctx context.Context, _ model.PersonID) error {
	const op = "app.alert.SuperuserSignedIn"
	a := l.a
	n, ok := a.notifiers[accountsOwner]
	if a.notify == nil || a.authz == nil || a.settings == nil || !ok {
		return errs.E(op, errs.Unsupported, errors.New("notify is not wired, so the alert cannot be sent"))
	}
	reader, err := a.settings.Scoped(notifySettings)
	if err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}
	seams := alertSeams{
		holders:  a.authz,
		notifier: n,
		sender:   a.notify,
		addresses: func(ctx context.Context) ([]model.Email, error) {
			v, err := reader.Text(ctx, "alert_addresses")
			if err != nil {
				return nil, fmt.Errorf("notify.alert_addresses: %w", err)
			}
			return notify.ParseAddresses(v) //nolint:wrapcheck // settings validated it; the caller wraps
		},
	}
	actx, cancel := context.WithTimeout(context.WithoutCancel(ctx), alertTimeout)
	defer cancel()
	return seams.send(actx)
}

// alertSeams are what the alert reads and sends through; tests replace them.
type alertSeams struct {
	holders interface {
		HoldersOf(ctx context.Context, p authz.Permission) ([]model.PersonID, error)
	}
	notifier  notify.Notifier      // scoped "identity"
	sender    notify.AddressSender // sends to one explicit address
	addresses func(ctx context.Context) ([]model.Email, error)
}

// send tells every recipient it can and returns what failed, joined under the kind of the
// first failure: errs.Unsupported when e-mail is not configured or there is nobody to tell,
// errs.Unavailable wrapping db.ErrReadOnly on a read-only copy, and the kinds of authz,
// settings and notify. A failure to find the holders or the addresses does not stop the
// other group.
func (s alertSeams) send(ctx context.Context) error {
	const op = "app.alert.send"
	var failed []error
	holders, err := s.holders.HoldersOf(ctx, identity.PermissionAccountsManage)
	if err != nil {
		failed = append(failed, fmt.Errorf("holders: %w", err))
	}
	addrs, err := s.addresses(ctx)
	if err != nil {
		failed = append(failed, fmt.Errorf("alert addresses: %w", err))
	}
	if len(holders) == 0 && len(addrs) == 0 && len(failed) == 0 {
		return errs.E(op, errs.Unsupported, errors.New("no holder of accounts.manage and no alert address: nobody to tell"))
	}
	m := notify.Message{Template: templateSuperuserSignedIn, Link: alertLink}
	if len(holders) > 0 {
		hm := m
		hm.To = holders
		if err := s.notifier.Send(ctx, hm); err != nil {
			failed = append(failed, fmt.Errorf("send to holders: %w", err))
		}
	}
	for _, to := range addrs {
		if err := s.sender.SendTo(ctx, to, m); err != nil {
			failed = append(failed, fmt.Errorf("send to an alert address: %w", err))
		}
	}
	if len(failed) == 0 {
		return nil
	}
	return errs.E(op, errs.KindOf(failed[0]), errors.Join(failed...))
}
