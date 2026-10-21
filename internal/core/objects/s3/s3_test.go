package s3_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"net"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/core/objects"
	"github.com/zedgama3/bms/internal/core/objects/s3"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/secret"
	"github.com/zedgama3/bms/internal/lib/validate"
)

func goodConfig() s3.Config {
	return s3.Config{
		Endpoint:  "storage.example.invalid:9000",
		Bucket:    "bms",
		Region:    "us-east-1",
		PathStyle: true,
		AccessKey: secret.New("access-key-0773"),
		SecretKey: secret.New("secret-key-0773"),
	}
}

func TestS3Config_Valid(t *testing.T) {
	if err := goodConfig().Validate(); err != nil {
		t.Fatalf("Validate(good) = %v", err)
	}
	c := goodConfig()
	c.Prefix = "a/b/"
	c.Timeout = 5 * time.Second
	if err := c.Validate(); err != nil {
		t.Fatalf("Validate(with prefix and timeout) = %v", err)
	}
	c = goodConfig()
	c.Region, c.Timeout = "", 0 // defaults: us-east-1, 30 s
	if err := c.Validate(); err != nil {
		t.Fatalf("Validate(defaults) = %v", err)
	}
}

func TestS3Config_Missing_ReturnsInvalid(t *testing.T) { // F57
	tests := []struct {
		name string
		mut  func(*s3.Config)
	}{
		{"no endpoint", func(c *s3.Config) { c.Endpoint = "" }},
		{"no bucket", func(c *s3.Config) { c.Bucket = "" }},
		{"no access key", func(c *s3.Config) { c.AccessKey = secret.Secret{} }},
		{"no secret key", func(c *s3.Config) { c.SecretKey = secret.Secret{} }},
		{"empty access key", func(c *s3.Config) { c.AccessKey = secret.New("") }},
		{"negative timeout", func(c *s3.Config) { c.Timeout = -time.Second }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := goodConfig()
			tc.mut(&c)
			err := c.Validate()
			if !errs.Is(err, errs.Invalid) {
				t.Fatalf("Validate = %v, want Invalid", err)
			}
			if _, err := s3.New(c); !errs.Is(err, errs.Invalid) {
				t.Errorf("New = %v, want Invalid", err)
			}
		})
	}
}

func TestS3Config_AllMissing_ReportsEveryProblemAtOnce(t *testing.T) {
	err := s3.Config{}.Validate()
	if !errs.Is(err, errs.Invalid) {
		t.Fatalf("Validate = %v, want Invalid", err)
	}
	if n := len(validate.Fields(err)); n < 4 {
		t.Errorf("Validate named %d problems, want at least 4 (endpoint, bucket, access key, secret key)", n)
	}
}

func TestS3Config_NeverPrintsSecrets(t *testing.T) {
	c := goodConfig()
	err := c.Validate()
	if err != nil {
		t.Fatal(err)
	}
	c.Bucket = ""
	err = c.Validate()
	if err == nil {
		t.Fatal("want an error")
	}
	for _, s := range []string{"access-key-0773", "secret-key-0773"} {
		if bytes.Contains([]byte(err.Error()), []byte(s)) {
			t.Errorf("error leaks a credential: %v", err)
		}
	}
}

func TestS3New_DoesNoNetworkIO(t *testing.T) {
	s, err := s3.New(goodConfig()) // the endpoint does not resolve; New must still succeed
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if s.Kind() != objects.S3 {
		t.Errorf("Kind = %v, want S3", s.Kind())
	}
}

// F62 (no server needed): nothing listens on the endpoint.
func TestS3_Unreachable_ReturnsUnavailable(t *testing.T) {
	c := goodConfig()
	ln, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0") // grab a free port, then close it: connection refused
	if err != nil {
		t.Fatal(err)
	}
	c.Endpoint = ln.Addr().String()
	if err := ln.Close(); err != nil {
		t.Fatal(err)
	}
	c.Timeout = 2 * time.Second
	s, err := s3.New(c)
	if err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	if err := s.Ping(ctx); !errs.Is(err, errs.Unavailable) {
		t.Errorf("Ping err = %v, want Unavailable", err)
	}
	data := []byte("x")
	if _, err := s.Put(ctx, objects.PutInput{Bucket: "files", Key: "k", Size: 1, SHA256: sha256.Sum256(data), Body: bytes.NewReader(data)}); !errs.Is(err, errs.Unavailable) {
		t.Errorf("Put err = %v, want Unavailable", err)
	}
	if _, err := s.Get(ctx, "files", "k"); !errs.Is(err, errs.Unavailable) {
		t.Errorf("Get err = %v, want Unavailable", err)
	}
	if _, err := s.Head(ctx, "files", "k"); !errs.Is(err, errs.Unavailable) {
		t.Errorf("Head err = %v, want Unavailable", err)
	}
	if err := s.Delete(ctx, "files", "k"); !errs.Is(err, errs.Unavailable) {
		t.Errorf("Delete err = %v, want Unavailable", err)
	}
	if err := s.List(ctx, "files", "", func(objects.Entry) error { return nil }); !errs.Is(err, errs.Unavailable) {
		t.Errorf("List err = %v, want Unavailable", err)
	}
}

// F62: a store that accepts the connection and never answers is "unreachable" too. The call's
// own Timeout firing (the caller's ctx is still alive) is Unavailable, not the caller's Timeout.
func TestS3_SilentEndpoint_ReturnsUnavailableNotTimeout(t *testing.T) {
	ln, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() }) //nolint:errcheck // test
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			t.Cleanup(func() { _ = c.Close() }) //nolint:errcheck // test
		}
	}()
	c := goodConfig()
	c.Endpoint = ln.Addr().String()
	c.Timeout = 300 * time.Millisecond
	s, err := s3.New(c)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	if err := s.Ping(ctx); !errs.Is(err, errs.Unavailable) {
		t.Errorf("Ping err = %v (kind %v), want Unavailable", err, errs.KindOf(err))
	}
	if _, err := s.Head(ctx, "files", "k"); !errs.Is(err, errs.Unavailable) {
		t.Errorf("Head err = %v (kind %v), want Unavailable", err, errs.KindOf(err))
	}
}
