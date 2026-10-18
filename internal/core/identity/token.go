package identity

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"math"
	"time"

	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
)

// The token, before base64url encoding (identity.md "The token"):
//
//	version(1) | account_id(16) | session_id(16) | generation(4) | issued_at(8) | expires_at(8) | mac(32)
//
// 85 bytes; the MAC covers the first 53. Nothing in it is secret except the MAC and nothing
// in it is personal.
const (
	tokenBodyLen = 1 + 16 + 16 + 4 + 8 + 8
	tokenLen     = tokenBodyLen + sha256.Size
)

// claims is what a verified token says.
type claims struct {
	account    model.AccountID
	session    SessionID
	generation int32
	issued     time.Time
	expires    time.Time
}

// rejection says why a token was refused, for the sessions_rejected metric.
type rejection struct {
	reason string // "malformed" or "expired"
}

func (r *rejection) Error() string { return "session token " + r.reason }

// sign returns the cookie value for c: the unpadded base64url form of the token.
func (s *Service) sign(c claims) string {
	b := make([]byte, tokenLen)
	b[0] = TokenVersion
	copy(b[1:17], c.account[:])
	copy(b[17:33], c.session[:])
	binary.BigEndian.PutUint32(b[33:37], uint32(c.generation))     //nolint:gosec // generation is never negative (CHECK >= 0)
	binary.BigEndian.PutUint64(b[37:45], uint64(c.issued.Unix()))  //nolint:gosec // times are after 1970
	binary.BigEndian.PutUint64(b[45:53], uint64(c.expires.Unix())) //nolint:gosec // times are after 1970
	copy(b[tokenBodyLen:], s.mac(b[:tokenBodyLen]))
	return base64.RawURLEncoding.EncodeToString(b)
}

func (s *Service) mac(body []byte) []byte {
	m := hmac.New(sha256.New, s.key)
	m.Write(body) // hash.Hash.Write never returns an error
	return m.Sum(nil)
}

// verifyToken checks the token's shape, version and MAC (constant-time), then its times
// against now: issued no later than now + ClockSkew, expires after now. It fails
// errs.Unauthenticated wrapping a *rejection naming the reason.
func (s *Service) verifyToken(tok string, now time.Time) (claims, error) {
	const op = "identity.Service.verifyToken"
	reject := func(reason string) (claims, error) {
		return claims{}, errs.E(op, errs.Unauthenticated, &rejection{reason: reason})
	}
	if len(tok) != base64.RawURLEncoding.EncodedLen(tokenLen) {
		return reject("malformed")
	}
	b, err := base64.RawURLEncoding.Strict().DecodeString(tok)
	if err != nil || len(b) != tokenLen || b[0] != TokenVersion {
		return reject("malformed")
	}
	if !hmac.Equal(b[tokenBodyLen:], s.mac(b[:tokenBodyLen])) {
		return reject("malformed")
	}
	var c claims
	copy(c.account[:], b[1:17])
	copy(c.session[:], b[17:33])
	gen := binary.BigEndian.Uint32(b[33:37])
	issued := binary.BigEndian.Uint64(b[37:45])
	expires := binary.BigEndian.Uint64(b[45:53])
	if gen > math.MaxInt32 || issued > math.MaxInt64 || expires > math.MaxInt64 {
		return reject("malformed")
	}
	c.generation = int32(gen)
	c.issued = time.Unix(int64(issued), 0).UTC()
	c.expires = time.Unix(int64(expires), 0).UTC()
	switch {
	case c.account.IsZero() || c.session.IsZero():
		return reject("malformed")
	case c.issued.After(now.Add(ClockSkew)):
		return reject("malformed")
	case !c.expires.After(now):
		return reject("expired")
	}
	return c, nil
}

// reasonOf extracts the rejection reason from a verifyToken error.
func reasonOf(err error) string {
	var r *rejection
	if errors.As(err, &r) {
		return r.reason
	}
	return "malformed"
}
