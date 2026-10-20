package settings

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/secret"
)

// Reader reads one area's settings by bare key. Every method fails errs.Invalid for a key
// the area did not declare or a kind that does not match, and with the first load's store
// kinds (Unavailable, Timeout, Canceled) when no snapshot exists yet. After that a read is
// a map lookup: no I/O, no error but Secret's.
type Reader interface {
	Bool(ctx context.Context, key string) (bool, error)
	Int(ctx context.Context, key string) (int64, error)
	Duration(ctx context.Context, key string) (time.Duration, error)
	// Text returns a Text or an Enum value.
	Text(ctx context.Context, key string) (string, error)
	// Secret unseals a Secret value; secret.Secret{} when unset. Fails errs.Unsupported
	// when the deployment has no sealing key and a value is set, errs.Internal when the
	// stored value cannot be unsealed (the key changed).
	Secret(ctx context.Context, key string) (secret.Secret, error)
}

// Scoped returns the Reader for one declared area. Fails errs.Invalid for an unknown area.
func (s *Service) Scoped(area string) (Reader, error) {
	if _, ok := s.areaIdx[area]; !ok {
		return nil, fieldError("area", codeUnknown, nil, "no such settings area")
	}
	return reader{s: s, area: area}, nil
}

type reader struct {
	s    *Service
	area string
}

var _ Reader = reader{}

// lookup resolves a bare key of the area and checks that its kind is one of kinds.
func (r reader) lookup(op, key string, kinds ...Kind) (entry, error) {
	e, ok := r.s.index[r.area+"."+key]
	if !ok {
		return entry{}, errs.E(op, errs.Invalid, fieldErrorf("key", codeUnknown, "the area declares no such setting"))
	}
	if !slices.Contains(kinds, e.decl.Kind) {
		return entry{}, errs.E(op, errs.Invalid, fieldErrorf("key", codeMalformed, "the setting is a %s, not read as that kind", e.decl.Kind))
	}
	return e, nil
}

func fieldErrorf(field, code, format string, args ...any) error {
	return fieldError(field, code, nil, fmt.Sprintf(format, args...))
}

// text returns the effective text of a plain setting: the stored value, or the default when
// none is stored or the stored one no longer parses (warned once per key, counted).
func (r reader) text(ctx context.Context, e entry) (string, error) {
	sn, err := r.s.current(ctx)
	if err != nil {
		return "", err
	}
	v, stale := r.s.effective(sn, e)
	if stale {
		r.s.noteStale(ctx, e.full)
	}
	return v, nil
}

// effective returns the stored value of e in sn when it parses, else the default; stale is
// true when a stored value was dropped for not parsing.
func (s *Service) effective(sn *snapshot, e entry) (text string, stale bool) {
	stored, ok := sn.values[e.full]
	if !ok {
		return e.decl.Default, false
	}
	if e.decl.Kind != Secret {
		stored = strings.TrimSpace(stored)
	}
	if shapeOf(e.decl, stored).code != "" {
		return e.decl.Default, true
	}
	return stored, false
}

// noteStale warns once per key per process and counts every fallback (decision 5).
func (s *Service) noteStale(ctx context.Context, full string) {
	s.metrics.staleRead(ctx)
	if _, seen := s.warned.LoadOrStore(full, struct{}{}); !seen {
		s.logger.WarnContext(ctx, "a stored setting no longer parses; using its default", slog.String("setting", full))
	}
}

func (r reader) Bool(ctx context.Context, key string) (bool, error) {
	const op = "settings.Reader.Bool"
	e, err := r.lookup(op, key, Bool)
	if err != nil {
		return false, err
	}
	v, err := r.text(ctx, e)
	if err != nil {
		return false, wrap(op, err)
	}
	b, perr := strconv.ParseBool(v)
	if perr != nil {
		return false, errs.E(op, errs.Internal, errors.New("a declared default does not parse"))
	}
	return b, nil
}

func (r reader) Int(ctx context.Context, key string) (int64, error) {
	const op = "settings.Reader.Int"
	e, err := r.lookup(op, key, Int)
	if err != nil {
		return 0, err
	}
	v, err := r.text(ctx, e)
	if err != nil {
		return 0, wrap(op, err)
	}
	n, perr := strconv.ParseInt(v, 10, 64)
	if perr != nil {
		return 0, errs.E(op, errs.Internal, errors.New("a declared default does not parse"))
	}
	return n, nil
}

func (r reader) Duration(ctx context.Context, key string) (time.Duration, error) {
	const op = "settings.Reader.Duration"
	e, err := r.lookup(op, key, Duration)
	if err != nil {
		return 0, err
	}
	v, err := r.text(ctx, e)
	if err != nil {
		return 0, wrap(op, err)
	}
	d, perr := time.ParseDuration(v)
	if perr != nil {
		return 0, errs.E(op, errs.Internal, errors.New("a declared default does not parse"))
	}
	return d, nil
}

func (r reader) Text(ctx context.Context, key string) (string, error) {
	const op = "settings.Reader.Text"
	e, err := r.lookup(op, key, Text, Enum)
	if err != nil {
		return "", err
	}
	v, err := r.text(ctx, e)
	return v, wrap(op, err)
}

func (r reader) Secret(ctx context.Context, key string) (secret.Secret, error) {
	const op = "settings.Reader.Secret"
	e, err := r.lookup(op, key, Secret)
	if err != nil {
		return secret.Secret{}, err
	}
	sn, err := r.s.current(ctx)
	if err != nil {
		return secret.Secret{}, wrap(op, err)
	}
	blob, ok := sn.sealed[e.full]
	if !ok {
		return secret.Secret{}, nil
	}
	if r.s.sealer == nil {
		return secret.Secret{}, errs.E(op, errs.Unsupported, errors.New("a secret is set but the deployment has no settings.sealing_key"))
	}
	plain, err := r.s.sealer.open(e.full, blob)
	if err != nil {
		return secret.Secret{}, wrap(op, err)
	}
	return secret.New(plain), nil
}
