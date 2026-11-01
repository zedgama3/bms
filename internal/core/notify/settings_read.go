package notify

import (
	"context"
	"fmt"
	"time"

	"github.com/zedgama3/bms/internal/core/notify/channel"
	"github.com/zedgama3/bms/internal/core/settings"
)

// SMTPFromSettings reads the notify.* SMTP keys through r, a Reader scoped "notify", and
// maps them onto SMTPConfig. The password is the sealed smtp_password, unsealed per read and
// never logged. A key that was never set reads as settings' declared default. It fails with
// the Reader's kinds, and errs.Invalid for a stored value notify cannot read (settings
// validated it, so that is a bug). The service calls it per use case and never caches the
// answer; app's `bms check` calls it until notify.Probe replaces that line (boundaries
// step 10).
func SMTPFromSettings(ctx context.Context, r settings.Reader) (SMTPConfig, error) {
	const op = "notify.SMTPFromSettings"
	var (
		cfg  SMTPConfig
		text = func(key string) (string, error) {
			v, err := r.Text(ctx, key)
			if err != nil {
				return "", fmt.Errorf("%s: notify.%s: %w", op, key, err)
			}
			return v, nil
		}
		err error
	)
	if cfg.Host, err = text("smtp_host"); err != nil {
		return SMTPConfig{}, err
	}
	port, err := r.Int(ctx, "smtp_port")
	if err != nil {
		return SMTPConfig{}, fmt.Errorf("%s: notify.smtp_port: %w", op, err)
	}
	cfg.Port = int(port)
	sec, err := text("smtp_security")
	if err != nil {
		return SMTPConfig{}, err
	}
	if cfg.Security, err = channel.ParseSecurity(sec); err != nil {
		return SMTPConfig{}, fmt.Errorf("%s: %w", op, err)
	}
	if cfg.Username, err = text("smtp_username"); err != nil {
		return SMTPConfig{}, err
	}
	if cfg.Password, err = r.Secret(ctx, "smtp_password"); err != nil {
		return SMTPConfig{}, fmt.Errorf("%s: notify.smtp_password: %w", op, err)
	}
	sender, err := text("sender_address")
	if err != nil {
		return SMTPConfig{}, err
	}
	if cfg.Sender.Email, err = ParseAddress(sender); err != nil {
		return SMTPConfig{}, fmt.Errorf("%s: notify.sender_address: %w", op, err)
	}
	if cfg.Sender.Name, err = text("sender_name"); err != nil {
		return SMTPConfig{}, err
	}
	reply, err := text("reply_to")
	if err != nil {
		return SMTPConfig{}, err
	}
	replyTo, err := ParseAddress(reply)
	if err != nil {
		return SMTPConfig{}, fmt.Errorf("%s: notify.reply_to: %w", op, err)
	}
	if replyTo != "" {
		cfg.ReplyTo = &Address{Email: replyTo}
	}
	alerts, err := text("alert_addresses")
	if err != nil {
		return SMTPConfig{}, err
	}
	if cfg.AlertAddresses, err = ParseAddresses(alerts); err != nil {
		return SMTPConfig{}, fmt.Errorf("%s: notify.alert_addresses: %w", op, err)
	}
	return cfg, nil
}

// retentionFrom reads notify.retention through r. It fails with the Reader's kinds.
func retentionFrom(ctx context.Context, r settings.Reader) (time.Duration, error) {
	d, err := r.Duration(ctx, "retention")
	if err != nil {
		return 0, fmt.Errorf("notify.retentionFrom: notify.retention: %w", err)
	}
	return d, nil
}
