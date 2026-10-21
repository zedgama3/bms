package config

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"syscall"

	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/secret"
	"github.com/zedgama3/bms/internal/lib/validate"
)

// Load reads environ (entries "NAME=value", as os.Environ returns them), applies defaults,
// parses every known key, reads *_FILE variants for secrets and the secrets file if it
// exists, validates, and returns the Config or one error that joins every problem found
// (F1–F29). Each problem is a validate.FieldError whose Field is the environment variable
// to fix (BMS_SECRETS_FILE for the secrets file) and whose Reason never contains a value.
// A missing secrets file is not a problem here: Session.Key is left unset for the command
// to judge. ctx bounds the file reads. Safe for concurrent use.
//
// Load returns the zero Config and errs.Invalid for configuration problems, errs.Internal
// for an environ entry without "=" (F12), and errs.Canceled or errs.Timeout if ctx ends
// before or during a file read (F13).
// Consumers: cmd/bms (once per process); app's tests with a hand-made environ.
func Load(ctx context.Context, environ []string) (Config, error) {
	const op = "config.Load"
	if err := ctxErr(ctx, op); err != nil {
		return Config{}, err
	}
	env, err := splitEnviron(environ)
	if err != nil {
		return Config{}, errs.E(op, errs.Internal, err)
	}

	l := loader{env: env, cfg: Default(), failed: make(map[int]bool)}
	if err := l.readKeys(ctx); err != nil {
		return Config{}, err
	}
	l.resolveFilesDir()
	if err := l.readSecretsFile(ctx); err != nil {
		return Config{}, err
	}
	for _, p := range l.cfg.problems() {
		if !l.failed[positionOf(p.field)] {
			l.probs = append(l.probs, p)
		}
	}
	l.probs = append(l.probs, l.unknownVariables()...)

	if len(l.probs) > 0 {
		sort.SliceStable(l.probs, func(i, j int) bool {
			return positionOf(l.probs[i].field) < positionOf(l.probs[j].field)
		})
		acc := validate.Errors{Op: op}
		for _, p := range l.probs {
			acc.Add(p.err)
		}
		return Config{}, acc.Err() //nolint:wrapcheck // Err already carries Op and errs.Invalid
	}
	return l.cfg, nil
}

// ctxErr returns nil while ctx is live, else Canceled or Timeout.
func ctxErr(ctx context.Context, op string) error {
	err := ctx.Err()
	if err == nil {
		return nil
	}
	kind := errs.Canceled
	if errors.Is(err, context.DeadlineExceeded) {
		kind = errs.Timeout
	}
	return errs.E(op, kind, err)
}

// splitEnviron turns "NAME=value" entries into a map; later entries win. The entry itself
// is never put in the error: it may hold a secret.
func splitEnviron(environ []string) (map[string]string, error) {
	env := make(map[string]string, len(environ))
	for i, e := range environ {
		name, value, ok := strings.Cut(e, "=")
		if !ok {
			return nil, fmt.Errorf("environ entry %d has no '='", i)
		}
		env[name] = value
	}
	return env, nil
}

// loader is the state of one Load call.
type loader struct {
	env    map[string]string
	cfg    Config
	probs  []problem
	failed map[int]bool // catalogue positions that already have a problem (F19)
}

func (l *loader) fail(field, code, format string, args ...any) {
	pos := positionOf(field)
	if l.failed[pos] {
		return
	}
	l.failed[pos] = true
	l.probs = append(l.probs, newProblem(field, code, format, args...))
}

// readKeys is step 4: every catalogue key from the environment, secrets through *_FILE too.
func (l *loader) readKeys(ctx context.Context) error {
	for _, e := range catalogue {
		text, set, err := l.value(ctx, e)
		if err != nil {
			return err
		}
		if !set {
			continue
		}
		if code, reason := e.parse(&l.cfg, text); code != "" {
			l.fail(e.Env, code, "%s", reason)
		}
	}
	return nil
}

// value returns the text of one key and whether it was set. A problem it records marks the
// key failed and reports it as unset.
func (l *loader) value(ctx context.Context, e entry) (text string, set bool, err error) {
	raw := l.env[e.Env]
	if !e.Secret {
		raw = strings.TrimSpace(raw)
		return raw, raw != "", nil
	}
	fileEnv := e.Env + fileSuffix
	path := strings.TrimSpace(l.env[fileEnv])
	if raw != "" && path != "" {
		l.fail(fileEnv, codeConflict, "set together with %s: set only one", e.Env)
		return "", false, nil
	}
	if path == "" {
		return raw, raw != "", nil
	}
	data, problem, err := l.readFile(ctx, path, fileEnv)
	if err != nil {
		return "", false, err
	}
	if problem {
		return "", false, nil
	}
	text = strings.TrimRight(string(data), "\r\n")
	return text, text != "", nil
}

// readFile reads at most MaxSecretFileBytes of path. It records a problem under field and
// reports problem=true when the file cannot be read or is too large; err is non-nil only
// when ctx ended.
func (l *loader) readFile(ctx context.Context, path, field string) (data []byte, problem bool, err error) {
	const op = "config.Load"
	if err := ctxErr(ctx, op); err != nil {
		return nil, false, err
	}
	data, rerr := readCapped(path)
	if rerr != nil {
		if err := ctxErr(ctx, op); err != nil {
			return nil, false, err
		}
		if errors.Is(rerr, errTooLarge) {
			l.fail(field, codeTooLarge, "file must be at most %d bytes", MaxSecretFileBytes)
		} else {
			l.fail(field, codeUnreadable, "cannot read the file: %s", reasonClass(rerr))
		}
		return nil, true, nil
	}
	if err := ctxErr(ctx, op); err != nil {
		return nil, false, err
	}
	return data, false, nil
}

var errTooLarge = errors.New("file too large")

// reasonClass names why a file could not be read, as a class only. The OS error text is
// dropped: it names the path, and an operator may have pasted a secret as the path.
func reasonClass(err error) string {
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return "not found"
	case errors.Is(err, fs.ErrPermission):
		return "permission denied"
	case errors.Is(err, syscall.EISDIR):
		return "is a directory"
	default:
		return "unreadable"
	}
}

// readCapped reads the file at path through an io.LimitReader. It returns the raw error;
// callers report only reasonClass of it, never the path or the OS text.
func readCapped(path string) (data []byte, err error) {
	f, err := os.Open(path) //nolint:gosec // G304: the path is the operator's to name
	if err != nil {
		return nil, err //nolint:wrapcheck // callers report only reasonClass(err)
	}
	defer func() {
		if cerr := f.Close(); cerr != nil {
			err = errors.Join(err, fmt.Errorf("close: %w", cerr))
		}
	}()
	return readAllCapped(f)
}

// readAllCapped reads r up to MaxSecretFileBytes and fails with errTooLarge beyond it.
func readAllCapped(r io.Reader) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, MaxSecretFileBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read: %w", err)
	}
	if len(data) > MaxSecretFileBytes {
		return nil, errTooLarge
	}
	return data, nil
}

// readSecrets opens the secrets file once, checks the open file's type and permissions,
// then reads it. A missing file returns nil data and no problem.
func (l *loader) readSecrets(ctx context.Context, path, field string) (data []byte, problem bool, err error) {
	const op = "config.Load"
	if err := ctxErr(ctx, op); err != nil {
		return nil, false, err
	}
	f, oerr := os.Open(path) //nolint:gosec // G304: the path is the operator's to name
	if errors.Is(oerr, fs.ErrNotExist) {
		return nil, false, nil
	}
	if oerr != nil {
		l.fail(field, codeUnreadable, "cannot read the secrets file: %s", reasonClass(oerr))
		return nil, true, nil
	}
	defer func() {
		if cerr := f.Close(); cerr != nil && err == nil && !problem {
			l.fail(field, codeUnreadable, "cannot close the secrets file: %s", reasonClass(cerr))
			problem = true
		}
	}()
	info, serr := f.Stat()
	switch {
	case serr != nil:
		l.fail(field, codeUnreadable, "cannot read the secrets file: %s", reasonClass(serr))
		return nil, true, nil
	case !info.Mode().IsRegular():
		l.fail(field, codeUnreadable, "must be a regular file")
		return nil, true, nil
	case runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0:
		l.fail(field, codeFilePerms, "must be readable by its owner only (chmod 0600)")
		return nil, true, nil
	}
	data, rerr := readAllCapped(f)
	if err := ctxErr(ctx, op); err != nil {
		return nil, false, err
	}
	switch {
	case errors.Is(rerr, errTooLarge):
		l.fail(field, codeTooLarge, "file must be at most %d bytes", MaxSecretFileBytes)
		return nil, true, nil
	case rerr != nil:
		l.fail(field, codeUnreadable, "cannot read the file: %s", reasonClass(rerr))
		return nil, true, nil
	}
	return data, false, nil
}

// secretsDoc is the shape of the secrets file. Unknown fields are ignored.
type secretsDoc struct {
	SessionKey         secret.Secret `json:"session_key"`
	SettingsSealingKey secret.Secret `json:"settings_sealing_key"`
}

// readSecretsFile is step 5: the generated secrets, a third source for the session key.
// A missing file is not a problem.
func (l *loader) readSecretsFile(ctx context.Context) error {
	const op = "config.Load"
	if err := ctxErr(ctx, op); err != nil {
		return err
	}
	// Leave a path Validate will reject to Validate: it holds no usable file.
	path := l.cfg.Secrets.File
	if explicit := strings.TrimSpace(l.env[envOf("secrets.file")]); explicit == "" {
		path = filepath.Join(l.cfg.Data.Dir, SecretsFileName)
		l.cfg.Secrets.File = path
	}
	if strings.ContainsRune(path, 0) || strings.TrimSpace(path) == "" {
		return nil
	}
	field := envOf("secrets.file")

	// Open once and check the open file, so the file that passed the checks is the one read.
	data, problem, err := l.readSecrets(ctx, path, field)
	if err != nil || problem || data == nil {
		return err
	}
	var doc secretsDoc
	if !bytes.HasPrefix(bytes.TrimSpace(data), []byte("{")) {
		l.fail(field, codeFormat, "must be a JSON object whose known fields are strings")
		return nil
	}
	if jerr := json.Unmarshal(data, &doc); jerr != nil {
		// The decoder's text may quote the content, so it is dropped; the reason states the rule.
		l.fail(field, codeFormat, "must be a JSON object whose known fields are strings")
		return nil //nolint:nilerr // recorded as a problem; Load goes on to report the rest
	}

	l.takeFromFile("session.key", field, SecretsFieldSessionKey, doc.SessionKey, &l.cfg.Session.Key)
	l.takeFromFile("settings.sealing_key", field, SecretsFieldSettingsSealingKey, doc.SettingsSealingKey, &l.cfg.Settings.SealingKey)
	return nil
}

// takeFromFile makes the secrets file's value the key's third source: it is used when no
// other source set the key, and it is a conflict when one did (F27, F32). A key the file
// does not hold, or one that already has a problem, is left alone.
func (l *loader) takeFromFile(key, field, fileField string, fromFile secret.Secret, dst *secret.Secret) {
	if !fromFile.IsSet() {
		return
	}
	keyEnv := envOf(key)
	switch {
	case l.failed[positionOf(keyEnv)]:
	case dst.IsSet():
		l.fail(keyEnv, codeConflict, "set in the environment and in %s (%s): keep only one", field, fileField)
	default:
		*dst = fromFile
	}
}

// unknownVariables is step 3: BMS_* names no key reads (F11), sorted so the report is stable.
func (l *loader) unknownVariables() []problem {
	var names []string
	for name := range l.env {
		upper := strings.ToUpper(name)
		if !strings.HasPrefix(upper, Prefix) || strings.HasPrefix(upper, TestPrefix) {
			continue
		}
		if _, known := order[name]; known {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]problem, 0, len(names))
	for _, n := range names {
		out = append(out, newProblem(n, codeUnknown, "unknown configuration variable"))
	}
	return out
}

// resolveFilesDir applies files.dir's default (<data.dir>/files) and makes the path absolute,
// as the disk store requires. A path Validate will reject is left for Validate.
func (l *loader) resolveFilesDir() {
	if explicit := strings.TrimSpace(l.env[envOf("files.dir")]); explicit == "" {
		l.cfg.Files.Dir = filepath.Join(l.cfg.Data.Dir, "files")
	}
	dir := l.cfg.Files.Dir
	if strings.TrimSpace(dir) == "" || strings.ContainsRune(dir, 0) {
		return
	}
	if abs, err := filepath.Abs(dir); err == nil {
		l.cfg.Files.Dir = abs
	}
}
