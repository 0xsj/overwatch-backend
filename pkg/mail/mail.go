package mail

import (
	"context"
	"fmt"
	"net"
	"net/smtp"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/0xsj/overwatch-backend/pkg/errors"
	"github.com/0xsj/overwatch-backend/pkg/secret"
)

var (
	ErrNoBaseURL = errors.New(errors.Invalid,
		"mail needs a base url: a link to the wrong host arrives, looks right, and does nothing")
	ErrNoAddr = errors.New(errors.Invalid, "mail needs an smtp address")
	ErrNoFrom = errors.New(errors.Invalid, "mail needs a from address")
)

// Message is what a Sender is handed. Text only — see the package doc.
type Message struct {
	To      string
	Subject string
	Body    string
}

type Sender interface {
	Send(ctx context.Context, m Message) error
}

type Config struct {
	Addr     string
	From     string
	BaseURL  string
	Username string
	Password secret.String
	Timeout  time.Duration
}

type Mailer struct {
	sender  Sender
	from    string
	baseURL *url.URL
}

// New wires a Mailer onto an SMTP server. It fails at construction rather than
// at the first send, so a misconfigured deployment does not boot.
func New(cfg Config) (*Mailer, error) {
	if cfg.Addr == "" {
		return nil, ErrNoAddr
	}
	sender := &SMTP{
		addr: cfg.Addr, from: cfg.From,
		username: cfg.Username, password: cfg.Password,
		timeout: cfg.Timeout,
	}
	return Wrap(sender, cfg.From, cfg.BaseURL)
}

// Wrap builds a Mailer on any Sender, which is how [Memory] gets used.
func Wrap(sender Sender, from, baseURL string) (*Mailer, error) {
	if sender == nil {
		return nil, errors.New(errors.Invalid, "mail: nil sender")
	}
	if from == "" {
		return nil, ErrNoFrom
	}
	if baseURL == "" {
		return nil, ErrNoBaseURL
	}
	base, err := url.Parse(baseURL)
	if err != nil || base.Scheme == "" || base.Host == "" {
		return nil, fmt.Errorf("mail: %q: %w", baseURL, ErrNoBaseURL)
	}
	return &Mailer{sender: sender, from: from, baseURL: base}, nil
}

// Link builds an absolute URL into the client. Every message that carries one
// builds it here, so there is one place the host can be wrong.
func (m *Mailer) Link(path string, query map[string]string) string {
	u := *m.baseURL
	u.Path = strings.TrimSuffix(u.Path, "/") + "/" + strings.TrimPrefix(path, "/")
	q := u.Query()
	for k, v := range query {
		q.Set(k, v)
	}
	u.RawQuery = q.Encode()
	return u.String()
}

func (m *Mailer) Verify(ctx context.Context, to, token string) error {
	link := m.Link("/verify", map[string]string{"token": token})
	return m.sender.Send(ctx, Message{
		To:      to,
		Subject: "Confirm your Overwatch address",
		Body: "Confirm this address to finish setting up your account:\n\n" +
			link + "\n\nIf you did not create an account, ignore this message — " +
			"nothing happens until the link is used.\n",
	})
}

func (m *Mailer) Reset(ctx context.Context, to, token string) error {
	link := m.Link("/reset", map[string]string{"token": token})
	return m.sender.Send(ctx, Message{
		To:      to,
		Subject: "Reset your Overwatch password",
		Body: "Use this link to choose a new password:\n\n" + link +
			"\n\nIf you did not ask for this, ignore it. Your current password " +
			"still works and nothing has changed.\n",
	})
}

// ConfirmEmail goes to the NEW address. Until this link is used the account is
// untouched, so a typo costs nothing — decisions/0021.
// Invite asks somebody to join an organisation. It names the org and the person
// who asked, because an unexpected invitation from a firm you have never heard
// of is indistinguishable from a phishing attempt otherwise.
func (m *Mailer) Invite(ctx context.Context, to, org, from, token string) error {
	link := m.Link("/invite", map[string]string{"token": token})
	return m.sender.Send(ctx, Message{
		To:      to,
		Subject: from + " invited you to " + org + " on Overwatch",
		Body: from + " has invited you to join " + org + " on Overwatch.\n\n" +
			link + "\n\n" +
			"You will need an Overwatch account for this address to accept. If you " +
			"do not have one, the link will offer to create it.\n\n" +
			"If you were not expecting this, ignore the message — nothing happens " +
			"until the link is used.\n",
	})
}

func (m *Mailer) ConfirmEmail(ctx context.Context, to, token string) error {
	link := m.Link("/email", map[string]string{"token": token})
	return m.sender.Send(ctx, Message{
		To:      to,
		Subject: "Confirm your new Overwatch address",
		Body: "Somebody asked to move an Overwatch account to this address.\n\n" +
			"Confirm it to finish:\n\n" + link + "\n\n" +
			"Until this link is used, nothing changes. If you were not expecting " +
			"this, ignore the message.\n",
	})
}

// EmailChangeRequested goes to the OLD address and carries NO LINK. That is the
// decision, not an omission — decisions/0021. A "wasn't me" link in a message
// sent to a possibly-hostile context is itself a takeover primitive, so the
// notice tells the reader what to do instead of doing it for them.
func (m *Mailer) EmailChangeRequested(ctx context.Context, to, proposed string) error {
	return m.sender.Send(ctx, Message{
		To:      to,
		Subject: "Someone asked to change your Overwatch address",
		Body: "A request was made to move your Overwatch account to " + proposed + ".\n\n" +
			"It has NOT happened yet. It takes effect only when that address is " +
			"confirmed.\n\n" +
			"If this was not you, sign in and change your password now — whoever " +
			"asked has access to a signed-in session.\n",
	})
}

// ── smtp ───────────────────────────────────────────────────────────────

type SMTP struct {
	addr     string
	from     string
	username string
	password secret.String
	timeout  time.Duration
}

func (s *SMTP) Send(ctx context.Context, m Message) error {
	timeout := s.timeout
	if timeout == 0 {
		timeout = 10 * time.Second
	}
	dialer := &net.Dialer{Timeout: timeout}
	conn, err := dialer.DialContext(ctx, "tcp", s.addr)
	if err != nil {
		return errors.Wrap(err, errors.Unavailable, "mail: dial "+s.addr)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(timeout))

	host, _, _ := net.SplitHostPort(s.addr)
	client, err := smtp.NewClient(conn, host)
	if err != nil {
		return errors.Wrap(err, errors.Unavailable, "mail: greet "+s.addr)
	}
	defer client.Close()

	if ok, _ := client.Extension("STARTTLS"); ok {
		if err := client.StartTLS(nil); err != nil {
			return errors.Wrap(err, errors.Unavailable, "mail: starttls")
		}
	}
	if s.username != "" {
		auth := smtp.PlainAuth("", s.username, s.password.Reveal(), host)
		if err := client.Auth(auth); err != nil {
			return errors.Wrap(err, errors.Unauthenticated, "mail: authenticate")
		}
	}
	if err := client.Mail(addressOf(s.from)); err != nil {
		return errors.Wrap(err, errors.Unavailable, "mail: from")
	}
	if err := client.Rcpt(m.To); err != nil {
		return errors.Wrap(err, errors.Invalid, "mail: recipient "+m.To)
	}
	w, err := client.Data()
	if err != nil {
		return errors.Wrap(err, errors.Unavailable, "mail: data")
	}
	if _, err := w.Write([]byte(render(s.from, m))); err != nil {
		return errors.Wrap(err, errors.Unavailable, "mail: write")
	}
	if err := w.Close(); err != nil {
		return errors.Wrap(err, errors.Unavailable, "mail: send")
	}
	return client.Quit()
}

func render(from string, m Message) string {
	var b strings.Builder
	fmt.Fprintf(&b, "From: %s\r\n", from)
	fmt.Fprintf(&b, "To: %s\r\n", m.To)
	fmt.Fprintf(&b, "Subject: %s\r\n", m.Subject)
	fmt.Fprintf(&b, "Date: %s\r\n", time.Now().Format(time.RFC1123Z))
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: text/plain; charset=utf-8\r\n\r\n")
	b.WriteString(strings.ReplaceAll(m.Body, "\n", "\r\n"))
	return b.String()
}

func addressOf(from string) string {
	if i := strings.LastIndex(from, "<"); i >= 0 {
		return strings.TrimSuffix(from[i+1:], ">")
	}
	return from
}

// ── memory ─────────────────────────────────────────────────────────────

// Memory keeps what it was given. It is a first-class adapter, not a double:
// running with no infrastructure is a supported mode, and a test asserting on a
// LINK rather than on a nil error is a test that would catch a broken one.
type Memory struct {
	mu   sync.Mutex
	sent []Message
}

func NewMemory() *Memory { return &Memory{} }

func (m *Memory) Send(_ context.Context, msg Message) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sent = append(m.sent, msg)
	return nil
}

// Reset forgets everything sent so far. A test asserting "these two messages
// went out" needs a known-empty mailbox, and filtering by recipient after the
// fact cannot tell a message from this step apart from an identical one from
// the last.
func (m *Memory) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sent = nil
}

func (m *Memory) Sent() []Message {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]Message(nil), m.sent...)
}

func (m *Memory) Last() (Message, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.sent) == 0 {
		return Message{}, false
	}
	return m.sent[len(m.sent)-1], true
}
