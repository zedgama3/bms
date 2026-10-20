package settings

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"

	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/secret"
)

// sealInfo is the HKDF info string; the "v1" is the algorithm's version.
const sealInfo = "bms settings seal v1"

// sealer seals and opens Secret values with AES-256-GCM under a key derived from the
// deployment's sealing key. The nonce is 12 random bytes written before the ciphertext, and
// the additional data is the setting's full key, so a ciphertext moved to another key fails
// to open. Safe for concurrent use.
type sealer struct {
	aead cipher.AEAD
}

// newSealer derives the AES key from key (HKDF-SHA256, no salt) once. It returns a nil
// sealer and no error for an unset key: such a deployment cannot set or read a Secret.
func newSealer(key secret.Secret) (*sealer, error) {
	const op = "settings.newSealer"
	if !key.IsSet() {
		return nil, nil //nolint:nilnil // nil means "no sealing key", which is valid
	}
	derived, err := hkdf.Key(sha256.New, []byte(key.Value()), nil, sealInfo, 32)
	if err != nil {
		return nil, errs.E(op, errs.Internal, errors.New("derive the sealing key"))
	}
	block, err := aes.NewCipher(derived)
	if err != nil {
		return nil, errs.E(op, errs.Internal, errors.New("build the cipher"))
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, errs.E(op, errs.Internal, errors.New("build the cipher mode"))
	}
	return &sealer{aead: aead}, nil
}

// seal returns nonce || ciphertext for plaintext under the full key. Fails errs.Internal
// when the system's random source fails.
func (s *sealer) seal(fullKey, plaintext string) ([]byte, error) {
	nonce := make([]byte, s.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, errs.E("settings.sealer.seal", errs.Internal, fmt.Errorf("random nonce for %s: %w", fullKey, err))
	}
	return s.aead.Seal(nonce, nonce, []byte(plaintext), []byte(fullKey)), nil
}

// open reverses seal. Fails errs.Internal, naming the key and nothing else, for a blob that
// is too short, was sealed under another key or for another setting, or was changed.
func (s *sealer) open(fullKey string, blob []byte) (string, error) {
	const op = "settings.sealer.open"
	n := s.aead.NonceSize()
	if len(blob) < n+s.aead.Overhead() {
		return "", errs.E(op, errs.Internal, fmt.Errorf("the sealed value of %s cannot be unsealed", fullKey))
	}
	plain, err := s.aead.Open(nil, blob[:n], blob[n:], []byte(fullKey))
	if err != nil {
		return "", errs.E(op, errs.Internal, fmt.Errorf("the sealed value of %s cannot be unsealed", fullKey))
	}
	return string(plain), nil
}
