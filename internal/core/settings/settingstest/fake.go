// Package settingstest gives modules a settings.Reader for their tests: Fake answers the
// declared defaults until a test sets a value, so a use case that reads a setting runs
// without a database. Use settingstest.NewFake(Manifest.Name, Manifest.Settings).
package settingstest

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/zedgama3/bms/internal/core/settings"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/secret"
)

// Fake implements settings.Reader over an area's declarations, in memory. Safe for
// concurrent use. It answers what the real service answers for what a test can set up:
// the default for a key nobody set, errs.Invalid for an undeclared key or a kind that does
// not match, and the value a test set. A test that sets what the real service would refuse
// (an undeclared key, a value that does not parse, a Secret through Set) is wrong, so the
// next read fails errs.Invalid and says so, instead of the mistake passing quietly.
type Fake struct {
	area  string
	decls map[string]settings.Decl

	mu      sync.Mutex
	values  map[string]string
	secrets map[string]secret.Secret
	misuse  error // the first invalid declaration or Set; every read returns it
}

var _ settings.Reader = (*Fake)(nil)

// NewFake returns a Fake for area over decls. An invalid declaration, or two with one key,
// makes every read fail errs.Invalid. Cannot fail.
func NewFake(area string, decls []settings.Decl) *Fake {
	f := &Fake{
		area: area, decls: make(map[string]settings.Decl, len(decls)),
		values: map[string]string{}, secrets: map[string]secret.Secret{},
	}
	for _, d := range decls {
		if err := d.Validate(); err != nil {
			f.misuse = fmt.Errorf("settingstest.NewFake(%q): declaration %q: %w", area, d.Key, err)
			continue
		}
		if _, dup := f.decls[d.Key]; dup {
			f.misuse = errs.E("settingstest.NewFake", errs.Invalid, fmt.Errorf("%s: %q is declared twice", area, d.Key))
			continue
		}
		f.decls[d.Key] = d
	}
	return f
}

// Set makes reads of key (a bare key) answer value, a text form as a regional would save
// it. Cannot fail; a mistake (an undeclared key, a Secret, a value that does not parse as
// the key's kind or fails its Check) is kept and fails the next read with errs.Invalid.
func (f *Fake) Set(key, value string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	d, ok := f.decls[key]
	switch {
	case !ok:
		f.fail(fmt.Errorf("settingstest.Fake.Set: %s declares no setting %q", f.area, key))
	case d.Kind == settings.Secret:
		f.fail(fmt.Errorf("settingstest.Fake.Set: %s.%s is a secret; use SetSecret", f.area, key))
	default:
		probe := d
		probe.Default = value
		if err := probe.Validate(); err != nil {
			f.fail(fmt.Errorf("settingstest.Fake.Set: the value of %s.%s does not fit its declaration: %w", f.area, key, err))
			return
		}
		f.values[key] = value
	}
}

// SetSecret makes reads of the Secret setting key answer s. Cannot fail; a mistake (an
// undeclared key or a key that is not a Secret) fails the next read with errs.Invalid.
func (f *Fake) SetSecret(key string, s secret.Secret) {
	f.mu.Lock()
	defer f.mu.Unlock()
	d, ok := f.decls[key]
	switch {
	case !ok:
		f.fail(fmt.Errorf("settingstest.Fake.SetSecret: %s declares no setting %q", f.area, key))
	case d.Kind != settings.Secret:
		f.fail(fmt.Errorf("settingstest.Fake.SetSecret: %s.%s is not a secret", f.area, key))
	default:
		f.secrets[key] = s
	}
}

// fail keeps the first mistake. The caller holds f.mu.
func (f *Fake) fail(err error) {
	if f.misuse == nil {
		f.misuse = errs.E("settingstest.Fake", errs.Invalid, err)
	}
}

// text returns the effective text of key, which must be declared with one of kinds.
func (f *Fake) text(op, key string, kinds ...settings.Kind) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.misuse != nil {
		return "", errs.E(op, errs.KindOf(f.misuse), f.misuse)
	}
	d, ok := f.decls[key]
	if !ok {
		return "", errs.E(op, errs.Invalid, fmt.Errorf("%s declares no setting %q", f.area, key))
	}
	match := false
	for _, k := range kinds {
		match = match || k == d.Kind
	}
	if !match {
		return "", errs.E(op, errs.Invalid, fmt.Errorf("%s.%s is a %s, not read as that kind", f.area, key, d.Kind))
	}
	if v, set := f.values[key]; set {
		return v, nil
	}
	return d.Default, nil
}

// Bool returns the setting as the real Reader does. Fails errs.Invalid for an undeclared
// key or another kind.
func (f *Fake) Bool(_ context.Context, key string) (bool, error) {
	const op = "settingstest.Fake.Bool"
	v, err := f.text(op, key, settings.Bool)
	if err != nil {
		return false, err
	}
	b, perr := strconv.ParseBool(v)
	if perr != nil {
		return false, errs.E(op, errs.Internal, errors.New("a declared value does not parse"))
	}
	return b, nil
}

// Int returns the setting as the real Reader does. Fails errs.Invalid for an undeclared
// key or another kind.
func (f *Fake) Int(_ context.Context, key string) (int64, error) {
	const op = "settingstest.Fake.Int"
	v, err := f.text(op, key, settings.Int)
	if err != nil {
		return 0, err
	}
	n, perr := strconv.ParseInt(v, 10, 64)
	if perr != nil {
		return 0, errs.E(op, errs.Internal, errors.New("a declared value does not parse"))
	}
	return n, nil
}

// Duration returns the setting as the real Reader does. Fails errs.Invalid for an
// undeclared key or another kind.
func (f *Fake) Duration(_ context.Context, key string) (time.Duration, error) {
	const op = "settingstest.Fake.Duration"
	v, err := f.text(op, key, settings.Duration)
	if err != nil {
		return 0, err
	}
	d, perr := time.ParseDuration(v)
	if perr != nil {
		return 0, errs.E(op, errs.Internal, errors.New("a declared value does not parse"))
	}
	return d, nil
}

// Text returns a Text or Enum setting as the real Reader does. Fails errs.Invalid for an
// undeclared key or another kind.
func (f *Fake) Text(_ context.Context, key string) (string, error) {
	return f.text("settingstest.Fake.Text", key, settings.Text, settings.Enum)
}

// Secret returns what SetSecret set, secret.Secret{} when nothing was. Fails errs.Invalid
// for an undeclared key or a key that is not a Secret.
func (f *Fake) Secret(_ context.Context, key string) (secret.Secret, error) {
	const op = "settingstest.Fake.Secret"
	if _, err := f.text(op, key, settings.Secret); err != nil {
		return secret.Secret{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.secrets[key], nil
}
