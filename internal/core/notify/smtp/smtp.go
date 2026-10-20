// Package smtp is notify's SMTP channel: RFC 5322 text messages over the standard library's
// net/smtp with STARTTLS, implicit TLS or no encryption, PLAIN or CRAM-MD5 authentication
// and a deadline for each conversation. It is the only package importing net/smtp.
//
// It imports notify/channel (not notify), model and lib. An error it returns never carries
// text from the server, which may echo an address: it carries a kind and a sentinel
// (channel.ErrRejected, channel.ErrAuth, channel.ErrTemporary) and nothing else.
package smtp

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/quotedprintable"
	"net"
	netmail "net/mail"
	netsmtp "net/smtp"
	"net/textproto"
	"strconv"
	"strings"
	"time"

	"github.com/zedgama3/bms/internal/core/notify/channel"
	"github.com/zedgama3/bms/internal/lib/clock"
	"github.com/zedgama3/bms/internal/lib/errs"
)

// Defaults of Options.
const (
	DefaultSendTimeout = 30 * time.Second
	DefaultHostname    = "localhost"
)

// Options tunes a channel. The zero value is usable.
type Options struct {
	// SendTimeout bounds one conversation (dial to quit); 0 means DefaultSendTimeout.
	SendTimeout time.Duration
	// Hostname is the name given in EHLO; "" means DefaultHostname.
	Hostname string
	// Clock stamps the Date header; nil means the system clock.
	Clock clock.Clock
	// TLSConfig is the base for STARTTLS and implicit TLS; nil means the system roots. The
	// ServerName is set to the host and MinVersion to TLS 1.2 when unset. Tests pass a
	// config trusting their own certificate.
	TLSConfig *tls.Config
}

// conversation stages, for classifying a server's reply.
const (
	stageConnect = iota
	stageHello
	stageStartTLS
	stageAuth
	stageMail
	stageRcpt
	stageData
)

type smtpChannel struct {
	cfg  channel.SMTPConfig
	opts Options
	tls  *tls.Config
}

// New builds a notify.Channel for cfg. It does no I/O. Fails errs.Invalid when cfg does not
// validate or is not configured.
func New(cfg channel.SMTPConfig, opts Options) (channel.Channel, error) {
	const op = "notify.smtp.New"
	if err := cfg.Validate(); err != nil {
		return nil, errs.E(op, errs.Invalid, err)
	}
	if !cfg.Configured() {
		return nil, errs.E(op, errs.Invalid, channel.ErrNotConfigured)
	}
	if opts.SendTimeout <= 0 {
		opts.SendTimeout = DefaultSendTimeout
	}
	if opts.Hostname == "" {
		opts.Hostname = DefaultHostname
	}
	if opts.Clock == nil {
		opts.Clock = clock.Real()
	}
	cfg.AlertAddresses = nil // not needed to send
	tc := &tls.Config{MinVersion: tls.VersionTLS12}
	if opts.TLSConfig != nil {
		tc = opts.TLSConfig.Clone()
		if tc.MinVersion == 0 {
			tc.MinVersion = tls.VersionTLS12
		}
	}
	tc.ServerName = cfg.Host
	return &smtpChannel{cfg: cfg, opts: opts, tls: tc}, nil
}

// Factory returns the channel.Factory over opts, for notify.Options.Channel.
func Factory(opts Options) channel.Factory {
	return func(cfg channel.SMTPConfig) (channel.Channel, error) { return New(cfg, opts) }
}

// session is one open conversation.
type session struct {
	c    *netsmtp.Client
	conn net.Conn
	stop func() bool
	done bool
}

// close releases the connection. After a failure it just drops it; after finish it is a
// no-op.
func (s *session) close() {
	s.stop()
	if s.done {
		return
	}
	if s.c != nil {
		_ = s.c.Close() //nolint:errcheck // the connection is being discarded; nothing to do with its error
		return
	}
	_ = s.conn.Close() //nolint:errcheck // as above
}

// finish says QUIT and closes. The work is already done by now (the message was accepted or
// the ping answered), so a failing QUIT only drops the connection.
func (s *session) finish() {
	s.stop()
	s.done = true
	if s.c.Quit() != nil {
		_ = s.c.Close() //nolint:errcheck // the conversation is over; nothing to do with its error
	}
}

// Ping connects, greets, secures, authenticates and quits.
func (ch *smtpChannel) Ping(ctx context.Context) error {
	const op = "notify.smtp.Ping"
	ctx, cancel := context.WithTimeout(ctx, ch.opts.SendTimeout)
	defer cancel()
	s, stage, err := ch.open(ctx)
	if err != nil {
		return classify(ctx, op, stage, err)
	}
	defer s.close()
	s.finish()
	return nil
}

// Send delivers e. Fails as channel.Channel documents.
func (ch *smtpChannel) Send(ctx context.Context, e channel.Envelope) error {
	const op = "notify.smtp.Send"
	data, err := ch.compose(e)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, ch.opts.SendTimeout)
	defer cancel()
	s, stage, err := ch.open(ctx)
	if err != nil {
		return classify(ctx, op, stage, err)
	}
	defer s.close()
	if err := s.c.Mail(string(ch.sender(e).Email)); err != nil {
		return classify(ctx, op, stageMail, err)
	}
	if err := s.c.Rcpt(string(e.To)); err != nil {
		return classify(ctx, op, stageRcpt, err)
	}
	w, err := s.c.Data()
	if err != nil {
		return classify(ctx, op, stageData, err)
	}
	if _, err := w.Write(data); err != nil {
		return classify(ctx, op, stageData, err)
	}
	if err := w.Close(); err != nil {
		return classify(ctx, op, stageData, err)
	}
	s.finish() // the server accepted the message; a failing QUIT does not undo it
	return nil
}

// open dials, reads the greeting, says EHLO, secures and authenticates. On failure it
// reports the stage that failed.
func (ch *smtpChannel) open(ctx context.Context) (*session, int, error) {
	addr := net.JoinHostPort(ch.cfg.Host, strconv.Itoa(ch.cfg.Port))
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, stageConnect, err //nolint:wrapcheck // classified by the caller, which drops the text
	}
	if dl, ok := ctx.Deadline(); ok {
		if err := conn.SetDeadline(dl); err != nil {
			_ = conn.Close()              //nolint:errcheck // already failing
			return nil, stageConnect, err //nolint:wrapcheck // classified by the caller
		}
	}
	s := &session{conn: conn}
	s.stop = context.AfterFunc(ctx, func() { _ = conn.Close() }) //nolint:errcheck // unblocks a stalled read; the error is the point

	if ch.cfg.Security == channel.TLS {
		tc := tls.Client(conn, ch.tls)
		if err := tc.HandshakeContext(ctx); err != nil {
			s.close()
			return nil, stageConnect, err //nolint:wrapcheck // classified by the caller
		}
		s.conn = tc
	}
	c, err := netsmtp.NewClient(s.conn, ch.cfg.Host)
	if err != nil {
		s.close()
		return nil, stageConnect, err //nolint:wrapcheck // classified by the caller
	}
	s.c = c
	if err := c.Hello(ch.opts.Hostname); err != nil {
		s.close()
		return nil, stageHello, err //nolint:wrapcheck // classified by the caller
	}
	if ch.cfg.Security == channel.StartTLS {
		if ok, _ := c.Extension("STARTTLS"); !ok {
			s.close()
			return nil, stageStartTLS, errors.New("STARTTLS is not offered")
		}
		if err := c.StartTLS(ch.tls); err != nil {
			s.close()
			return nil, stageStartTLS, err //nolint:wrapcheck // classified by the caller
		}
	}
	if ch.cfg.Username != "" && ch.cfg.Password.IsSet() {
		a, err := ch.authFor(c)
		if err != nil {
			s.close()
			return nil, stageAuth, err
		}
		if err := c.Auth(a); err != nil {
			s.close()
			return nil, stageAuth, err //nolint:wrapcheck // classified by the caller
		}
	}
	return s, stageData, nil
}

// authFor picks PLAIN when the server offers it, else CRAM-MD5.
func (ch *smtpChannel) authFor(c *netsmtp.Client) (netsmtp.Auth, error) {
	ok, params := c.Extension("AUTH")
	if !ok {
		return nil, errors.New("the server offers no authentication")
	}
	mechs := strings.Fields(strings.ToUpper(params))
	has := func(m string) bool {
		for _, x := range mechs {
			if x == m {
				return true
			}
		}
		return false
	}
	pw := ch.cfg.Password.Value()
	switch {
	case has("PLAIN"):
		return netsmtp.PlainAuth("", ch.cfg.Username, pw, ch.cfg.Host), nil
	case has("CRAM-MD5"):
		return netsmtp.CRAMMD5Auth(ch.cfg.Username, pw), nil
	default:
		return nil, errors.New("the server offers no supported authentication")
	}
}

// classify maps a failure at a stage to a kind and a sentinel, dropping the original text.
func classify(ctx context.Context, op string, stage int, err error) error {
	if cerr := ctx.Err(); cerr != nil {
		return errs.E(op, errs.KindOf(cerr), cerr)
	}
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return errs.E(op, errs.Timeout, context.DeadlineExceeded)
	}
	if stage == stageAuth {
		return errs.E(op, errs.Unavailable, channel.ErrAuth)
	}
	var te *textproto.Error
	if errors.As(err, &te) {
		switch {
		case te.Code >= 400 && te.Code < 500:
			return errs.E(op, errs.Unavailable, channel.ErrTemporary)
		case te.Code >= 500 && (stage == stageMail || stage == stageRcpt || stage == stageData):
			return errs.E(op, errs.Invalid, channel.ErrRejected)
		}
		return errs.E(op, errs.Unavailable, errors.New("the server refused the conversation"))
	}
	if stage == stageStartTLS {
		return errs.E(op, errs.Unavailable, errors.New("could not secure the connection"))
	}
	return errs.E(op, errs.Unavailable, errors.New("could not talk to the server"))
}

// compose renders e as an RFC 5322 message with CRLF line ends.
func (ch *smtpChannel) compose(e channel.Envelope) ([]byte, error) {
	const op = "notify.smtp.compose"
	if err := e.To.Validate(); err != nil {
		return nil, errs.E(op, errs.Invalid, err)
	}
	if strings.ContainsAny(e.Subject, "\r\n") || strings.ContainsAny(e.MessageID, "\r\n <>") ||
		strings.ContainsAny(ch.sender(e).Name, "\r\n") {
		return nil, errs.E(op, errs.Invalid, errors.New("a header value holds a line break"))
	}
	sender := ch.sender(e)
	from := netmail.Address{Name: sender.Name, Address: string(sender.Email)}
	var b bytes.Buffer
	header := func(k, v string) { fmt.Fprintf(&b, "%s: %s\r\n", k, v) }
	header("From", from.String())
	header("To", (&netmail.Address{Address: string(e.To)}).String())
	if rt := ch.replyTo(e); rt != nil {
		header("Reply-To", rt.String())
	}
	header("Subject", mime.QEncoding.Encode("utf-8", e.Subject))
	header("Date", ch.opts.Clock.Now().Format(time.RFC1123Z))
	if e.MessageID != "" {
		header("Message-ID", "<"+e.MessageID+">")
	}
	header("MIME-Version", "1.0")
	header("Content-Type", "text/plain; charset=utf-8")
	header("Content-Transfer-Encoding", "quoted-printable")
	header("Auto-Submitted", "auto-generated")
	b.WriteString("\r\n")
	qp := quotedprintable.NewWriter(&b)
	if _, err := io.WriteString(qp, e.Body); err != nil {
		return nil, errs.E(op, errs.Internal, err)
	}
	if err := qp.Close(); err != nil {
		return nil, errs.E(op, errs.Internal, err)
	}
	return b.Bytes(), nil
}

// sender returns the envelope's From, else the configured sender.
func (ch *smtpChannel) sender(e channel.Envelope) channel.Address {
	if e.From.Email != "" {
		return e.From
	}
	return ch.cfg.Sender
}

// replyTo returns the Reply-To address: the envelope's, else the configuration's.
func (ch *smtpChannel) replyTo(e channel.Envelope) *netmail.Address {
	a := e.ReplyTo
	if a == nil {
		a = ch.cfg.ReplyTo
	}
	if a == nil || a.Email == "" {
		return nil
	}
	return &netmail.Address{Name: a.Name, Address: string(a.Email)}
}
