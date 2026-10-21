//go:build integration

package objects_test

import (
	"crypto/rand"
	"encoding/hex"
	"os"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/core/objects"
	"github.com/zedgama3/bms/internal/core/objects/objectstest"
	"github.com/zedgama3/bms/internal/core/objects/s3"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/secret"
)

// s3Config reads the MinIO (or other S3-compatible) test endpoint from the environment
// (files.md, Test plan): BMS_TEST_S3_ENDPOINT, BMS_TEST_S3_BUCKET, BMS_TEST_S3_ACCESS_KEY and
// BMS_TEST_S3_SECRET_KEY. It skips, with the reason, when the endpoint is not configured; the
// compose profile `s3` (deploy/compose) provides one. Every call gets its own key prefix, so
// tests never see each other's objects.
func s3Config(t *testing.T) s3.Config {
	t.Helper()
	endpoint := os.Getenv("BMS_TEST_S3_ENDPOINT")
	if endpoint == "" {
		t.Skip("BMS_TEST_S3_ENDPOINT is not set: start the compose profile `s3` (MinIO) and export BMS_TEST_S3_* to run the s3 store tests")
	}
	var r [6]byte
	if _, err := rand.Read(r[:]); err != nil {
		t.Fatalf("rand: %v", err)
	}
	return s3.Config{
		Endpoint:  endpoint,
		Bucket:    os.Getenv("BMS_TEST_S3_BUCKET"),
		Region:    "us-east-1",
		Prefix:    "test-" + hex.EncodeToString(r[:]) + "/",
		PathStyle: true,
		AccessKey: secret.New(os.Getenv("BMS_TEST_S3_ACCESS_KEY")),
		SecretKey: secret.New(os.Getenv("BMS_TEST_S3_SECRET_KEY")),
		Timeout:   15 * time.Second,
	}
}

func s3Factory(t *testing.T) objects.Store {
	t.Helper()
	s, err := s3.New(s3Config(t))
	if err != nil {
		t.Fatalf("s3.New: %v", err)
	}
	return s
}

func TestObjects_S3_Contract(t *testing.T) {
	s3Config(t) // skip early with the reason
	t.Run("contract", func(t *testing.T) { runContract(t, s3Factory) })
	t.Run("Conformance", func(t *testing.T) { objectstest.Conformance(t, s3Factory) })
}

func TestS3_Kind(t *testing.T) {
	if k := s3Factory(t).Kind(); k != objects.S3 {
		t.Errorf("Kind = %v, want S3", k)
	}
}

// F54/F56: a bucket whose policy allows anonymous download fails Ping with Invalid wrapping
// ErrPublic. The bucket is prepared by the operator of the test stack (a policy of
// `download`) and named in BMS_TEST_S3_PUBLIC_BUCKET.
func TestS3_PublicBucket_PingReturnsInvalidErrPublic(t *testing.T) {
	cfg := s3Config(t)
	pub := os.Getenv("BMS_TEST_S3_PUBLIC_BUCKET")
	if pub == "" {
		t.Skip("BMS_TEST_S3_PUBLIC_BUCKET is not set: name a bucket whose policy was set to `download` to run the public-bucket probe")
	}
	cfg.Bucket = pub
	s, err := s3.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	err = s.Ping(t.Context())
	if !errs.Is(err, errs.Invalid) || !isErrPublic(err) {
		t.Fatalf("Ping err = %v, want Invalid wrapping ErrPublic", err)
	}
}

// The private bucket's probe passes and leaves no probe object behind.
func TestS3_PrivateBucket_PingOKLeavesNoProbe(t *testing.T) {
	s := s3Factory(t)
	if err := s.Ping(t.Context()); err != nil {
		t.Fatalf("Ping: %v", err)
	}
	if _, err := s.Head(t.Context(), "files", objects.ProbeKey); err == nil {
		t.Error("the probe object was left behind")
	}
}

// F62: an unreachable endpoint, wrong credentials and an absent bucket are all Unavailable.
func TestS3_Misconfigured_ReturnsUnavailable(t *testing.T) {
	base := s3Config(t)
	cases := map[string]func(*s3.Config){
		"wrong secret":  func(c *s3.Config) { c.SecretKey = secret.New("not-the-secret-0000") },
		"absent bucket": func(c *s3.Config) { c.Bucket = "bms-test-no-such-bucket-0773" },
	}
	for name, mut := range cases {
		t.Run(name, func(t *testing.T) {
			c := base
			mut(&c)
			s, err := s3.New(c)
			if err != nil {
				t.Fatal(err)
			}
			if err := s.Ping(t.Context()); !errs.Is(err, errs.Unavailable) {
				t.Errorf("Ping err = %v, want Unavailable", err)
			}
			if _, err := s.Head(t.Context(), "files", "k"); !errs.Is(err, errs.Unavailable) {
				t.Errorf("Head err = %v, want Unavailable", err)
			}
		})
	}
}
