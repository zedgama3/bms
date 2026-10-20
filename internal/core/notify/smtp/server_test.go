package smtp_test

import (
	"bufio"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"io"
	"math/big"
	"mime/quotedprintable"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

// server is a scripted SMTP server on a loopback listener. replies maps an upper-case verb
// ("GREETING", "EHLO", "AUTH", "MAIL", "RCPT", "DATA", "DATAEND", "QUIT") to a full reply
// line that replaces the default. stall names a verb at which the server goes silent until
// the test ends.
type server struct {
	t        *testing.T
	ln       net.Listener
	replies  map[string]string
	stall    string
	stalled  chan struct{}
	starttls bool
	authAd   bool
	implicit bool
	tlsCfg   *tls.Config

	done chan struct{}
	wg   sync.WaitGroup
	mu   sync.Mutex
	cmds []string
	msgs [][]byte
	conn int
}

func newServer(t *testing.T, mod func(*server)) *server {
	t.Helper()
	ln, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	s := &server{t: t, ln: ln, replies: map[string]string{}, done: make(chan struct{}), stalled: make(chan struct{}, 16)}
	if mod != nil {
		mod(s)
	}
	s.wg.Add(1)
	go s.accept()
	t.Cleanup(func() {
		close(s.done)
		if err := ln.Close(); err != nil {
			t.Logf("closing listener: %v", err)
		}
		s.wg.Wait()
	})
	return s
}

func (s *server) hostPort() (string, int) {
	a, ok := s.ln.Addr().(*net.TCPAddr)
	if !ok {
		s.t.Fatal("listener is not TCP")
	}
	return a.IP.String(), a.Port
}

func (s *server) commands() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.cmds...)
}

func (s *server) messages() [][]byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([][]byte(nil), s.msgs...)
}

func (s *server) connections() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.conn
}

func (s *server) saw(prefix string) bool {
	for _, c := range s.commands() {
		if strings.HasPrefix(strings.ToUpper(c), strings.ToUpper(prefix)) {
			return true
		}
	}
	return false
}

func (s *server) accept() {
	defer s.wg.Done()
	for {
		c, err := s.ln.Accept()
		if err != nil {
			return
		}
		s.mu.Lock()
		s.conn++
		s.mu.Unlock()
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			s.serve(c)
		}()
	}
}

func (s *server) reply(w *bufio.Writer, verb, def string) {
	line := def
	if r, ok := s.replies[verb]; ok {
		line = r
	}
	ignore(w.WriteString(line + "\r\n"))
	ignore(w.Flush())
}

func (s *server) maybeStall(verb string) bool {
	if s.stall != verb {
		return false
	}
	s.stalled <- struct{}{}
	<-s.done
	return true
}

func (s *server) serve(raw net.Conn) {
	defer func() { ignore(raw.Close()) }()
	go func() { // unblock reads when the test ends
		<-s.done
		ignore(raw.Close())
	}()
	conn := net.Conn(raw)
	if s.implicit {
		conn = tls.Server(raw, s.tlsCfg)
	}
	r, w := bufio.NewReader(conn), bufio.NewWriter(conn)
	if s.maybeStall("GREETING") {
		return
	}
	s.reply(w, "GREETING", "220 fake.example ESMTP ready")
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimRight(line, "\r\n")
		s.mu.Lock()
		s.cmds = append(s.cmds, line)
		s.mu.Unlock()
		verb := strings.ToUpper(strings.Fields(line + " ")[0])
		if s.maybeStall(verb) {
			return
		}
		switch verb {
		case "EHLO", "HELO":
			if v, ok := s.replies["EHLO"]; ok {
				ignore(w.WriteString(v + "\r\n"))
				ignore(w.Flush())
				continue
			}
			out := "250-fake.example\r\n"
			if s.starttls {
				out += "250-STARTTLS\r\n"
			}
			if s.authAd {
				out += "250-AUTH PLAIN LOGIN\r\n"
			}
			out += "250 8BITMIME\r\n"
			ignore(w.WriteString(out))
			ignore(w.Flush())
		case "STARTTLS":
			s.reply(w, "STARTTLS", "220 go ahead")
			if v, ok := s.replies["STARTTLS"]; ok && !strings.HasPrefix(v, "2") {
				continue
			}
			tc := tls.Server(conn, s.tlsCfg)
			if err := tc.HandshakeContext(context.Background()); err != nil {
				return
			}
			conn = tc
			r, w = bufio.NewReader(conn), bufio.NewWriter(conn)
		case "AUTH":
			s.reply(w, "AUTH", "235 2.7.0 accepted")
		case "MAIL":
			s.reply(w, "MAIL", "250 ok")
		case "RCPT":
			s.reply(w, "RCPT", "250 ok")
		case "RSET", "NOOP":
			s.reply(w, verb, "250 ok")
		case "DATA":
			s.reply(w, "DATA", "354 end with <CRLF>.<CRLF>")
			if v, ok := s.replies["DATA"]; ok && !strings.HasPrefix(v, "354") {
				continue
			}
			var body strings.Builder
			for {
				l, err := r.ReadString('\n')
				if err != nil {
					return
				}
				if l == ".\r\n" {
					break
				}
				l = strings.TrimPrefix(l, ".") // dot-unstuffing: one leading dot is dropped
				body.WriteString(l)
			}
			if s.maybeStall("DATAEND") {
				return
			}
			s.mu.Lock()
			s.msgs = append(s.msgs, []byte(body.String()))
			s.mu.Unlock()
			s.reply(w, "DATAEND", "250 queued")
		case "QUIT":
			s.reply(w, "QUIT", "221 bye")
			return
		default:
			s.reply(w, verb, "502 not implemented")
		}
	}
}

// selfSigned returns a TLS server config for 127.0.0.1 and the pool that trusts it.
func selfSigned(t *testing.T) (*tls.Config, *x509.CertPool) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "fake.example"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}, IsCA: true, BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("cert: %v", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(cert)
	return &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}}, MinVersion: tls.VersionTLS12}, pool
}

func newQP(r io.Reader) io.Reader { return quotedprintable.NewReader(r) }
