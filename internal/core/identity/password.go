package identity

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	_ "embed" // the common-password list
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"

	"golang.org/x/crypto/argon2"

	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/secret"
	"github.com/zedgama3/bms/internal/lib/validate"
)

// Argon2id parameters (decision 5, OWASP minimum m=19 MiB, t=2, p=1). They live in the
// stored PHC string, so they can be raised later with rehash-on-login.
const (
	argonMemory  = 19456 // KiB
	argonTime    = 2
	argonThreads = 1
	argonKeyLen  = 32
	argonSaltLen = 16

	// Bounds on parameters read back from a stored hash, so a damaged row cannot ask the
	// process for more than 256 MiB.
	maxArgonMemory  = 256 * 1024 // KiB
	maxArgonTime    = 16
	maxArgonThreads = 16
	minArgonKeyLen  = 16
	maxArgonKeyLen  = 64
)

// commonPasswordList is SecLists' xato-net-10-million-passwords-100000.txt (MIT); its
// licence and provenance are in passwords/.
//
//go:embed passwords/xato-net-10-million-passwords-100000.txt
var commonPasswordList string

var commonSet = sync.OnceValues(func() (map[string]struct{}, error) {
	const op = "identity.commonSet"
	if commonPasswordList == "" {
		return nil, errs.E(op, errs.Internal, errors.New("the embedded common-password list is empty"))
	}
	set := make(map[string]struct{}, 100_000)
	for line := range strings.Lines(commonPasswordList) {
		line = strings.TrimRight(line, "\r\n")
		if line != "" {
			set[line] = struct{}{}
		}
	}
	return set, nil
})

// addPasswordProblems adds to v what is wrong with pw as a new password under field:
// required (empty), too_long (over MaxPasswordBytes), too_short (fewer than min runes,
// with params min) and common (an exact match in the list). A too-long password is not
// looked up. Messages never echo the password.
func addPasswordProblems(v *validate.Errors, field, pw string, minLen int, common map[string]struct{}) {
	switch {
	case pw == "":
		v.Addf(field, codeRequired, "must be set")
		return
	case len(pw) > MaxPasswordBytes:
		v.Add(errs.E("", errs.Invalid, &validate.FieldError{
			Field: field, Code: codeTooLong, Params: map[string]string{"max": strconv.Itoa(MaxPasswordBytes)},
			Reason: fmt.Sprintf("must be at most %d bytes", MaxPasswordBytes),
		}))
		return
	}
	if utf8.RuneCountInString(pw) < minLen {
		v.Add(errs.E("", errs.Invalid, &validate.FieldError{
			Field: field, Code: codeTooShort, Params: map[string]string{"min": strconv.Itoa(minLen)},
			Reason: fmt.Sprintf("must be at least %d characters", minLen),
		}))
	}
	if _, bad := common[pw]; bad {
		v.Addf(field, codeCommon, "is too common; choose another")
	}
}

// acquire takes one of the HashConcurrency slots, waiting on ctx. The caller must call the
// returned release. Fails errs.Timeout or errs.Canceled when ctx ends first (F60).
func (s *Service) acquire(ctx context.Context) (release func(), err error) {
	const op = "identity.Service.acquire"
	if err := ctx.Err(); err != nil {
		return nil, errs.E(op, errs.KindOf(err), err)
	}
	select {
	case s.sem <- struct{}{}:
		return func() { <-s.sem }, nil
	case <-ctx.Done():
		return nil, errs.E(op, errs.KindOf(ctx.Err()), ctx.Err())
	}
}

// hash returns the Argon2id PHC string of pw with a fresh salt. At most HashConcurrency
// hashes run at once; the rest wait on ctx. Fails errs.Timeout/Canceled while waiting and
// errs.Internal when the random source fails.
func (s *Service) hash(ctx context.Context, pw string) (secret.Secret, error) {
	const op = "identity.Service.hash"
	release, err := s.acquire(ctx)
	if err != nil {
		return secret.Secret{}, err
	}
	defer release()
	salt := make([]byte, argonSaltLen)
	if _, err := io.ReadFull(s.rand, salt); err != nil {
		return secret.Secret{}, errs.E(op, errs.Internal, fmt.Errorf("read random salt: %w", err))
	}
	return secret.New(encodePHC(pw, salt)), nil
}

func encodePHC(pw string, salt []byte) string {
	key := argon2.IDKey([]byte(pw), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	enc := base64.RawStdEncoding
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, argonMemory, argonTime, argonThreads, enc.EncodeToString(salt), enc.EncodeToString(key))
}

// phc is a parsed stored hash.
type phc struct {
	memory  uint32
	time    uint32
	threads uint8
	salt    []byte
	key     []byte
}

// weaker reports whether the hash was made with parameters below the current ones, so
// SignIn should re-hash it.
func (p phc) weaker() bool {
	return p.memory < argonMemory || p.time < argonTime || p.threads < argonThreads || len(p.key) < argonKeyLen
}

var errBadHash = errors.New("the stored password hash is not an Argon2id PHC string")

func parsePHC(h string) (phc, error) {
	parts := strings.Split(h, "$")
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" {
		return phc{}, errBadHash
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return phc{}, errBadHash
	}
	var m, t uint32
	var p uint8
	if n, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &m, &t, &p); err != nil || n != 3 {
		return phc{}, errBadHash
	}
	if m == 0 || m > maxArgonMemory || t == 0 || t > maxArgonTime || p == 0 || p > maxArgonThreads {
		return phc{}, errBadHash
	}
	enc := base64.RawStdEncoding.Strict()
	salt, err := enc.DecodeString(parts[4])
	if err != nil || len(salt) == 0 {
		return phc{}, errBadHash
	}
	key, err := enc.DecodeString(parts[5])
	if err != nil || len(key) < minArgonKeyLen || len(key) > maxArgonKeyLen {
		return phc{}, errBadHash
	}
	return phc{memory: m, time: t, threads: p, salt: salt, key: key}, nil
}

// verify reports whether pw matches the stored hash, and whether the hash was made with
// weaker parameters than the current ones (so the caller may upgrade it). Constant-time
// comparison. Fails errs.Internal for a hash that does not parse, errs.Timeout/Canceled
// while waiting for a hash slot.
func (s *Service) verify(ctx context.Context, pw string, stored secret.Secret) (ok, rehash bool, err error) {
	const op = "identity.Service.verify"
	p, err := parsePHC(stored.Value())
	if err != nil {
		return false, false, errs.E(op, errs.Internal, err)
	}
	release, err := s.acquire(ctx)
	if err != nil {
		return false, false, err
	}
	defer release()
	got := argon2.IDKey([]byte(pw), p.salt, p.time, p.memory, p.threads, uint32(len(p.key))) //nolint:gosec // len is at most maxArgonKeyLen
	return subtle.ConstantTimeCompare(got, p.key) == 1, p.weaker(), nil
}

// dummyHash is verified when no account matched the submitted address, so the response
// takes as long as a real check. It is made once, with the current parameters.
var dummyHash = sync.OnceValue(func() secret.Secret {
	salt := make([]byte, argonSaltLen)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		// crypto/rand does not fail on a supported platform; a fixed salt is still a valid
		// dummy, since its only job is to cost the same.
		salt = []byte("bms-identity-dum")
	}
	return secret.New(encodePHC("dummy password for timing only", salt))
})

// generatePassword returns GeneratedPasswordLength characters of generatedAlphabet, drawn
// uniformly from the random source. Fails errs.Internal when the source fails (F43).
func (s *Service) generatePassword() (string, error) {
	const op = "identity.Service.generatePassword"
	const limit = 256 - 256%len(generatedAlphabet) // reject the biased tail
	out := make([]byte, 0, GeneratedPasswordLength)
	buf := make([]byte, GeneratedPasswordLength*2)
	for len(out) < GeneratedPasswordLength {
		if _, err := io.ReadFull(s.rand, buf); err != nil {
			return "", errs.E(op, errs.Internal, fmt.Errorf("read random bytes: %w", err))
		}
		for _, b := range buf {
			if int(b) >= limit {
				continue
			}
			out = append(out, generatedAlphabet[int(b)%len(generatedAlphabet)])
			if len(out) == GeneratedPasswordLength {
				break
			}
		}
	}
	return string(out), nil
}
