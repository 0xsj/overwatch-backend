package mail_test

import (
	"context"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/0xsj/overwatch-backend/pkg/errors"
	"github.com/0xsj/overwatch-backend/pkg/mail"
)

func mailer(t *testing.T) (*mail.Mailer, *mail.Memory) {
	t.Helper()
	mem := mail.NewMemory()
	m, err := mail.Wrap(mem, "Overwatch <no-reply@overwatch.test>", "http://localhost:7010")
	if err != nil {
		t.Fatal(err)
	}
	return m, mem
}

// The quiet failure this refuses to allow: a mail that arrives, looks right, and
// points at a host the recipient cannot reach. Nothing errors and nobody is
// told, so it fails at construction instead.
func TestAMailerWillNotExistWithoutSomewhereForLinksToPoint(t *testing.T) {
	mem := mail.NewMemory()
	for name, base := range map[string]string{
		"nothing":     "",
		"a bare host": "localhost:7010",
		"a path":      "/verify",
		"nonsense":    "://",
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := mail.Wrap(mem, "a@b.test", base); !errors.Is(err, mail.ErrNoBaseURL) {
				t.Errorf("Wrap(%q) gave %v, want ErrNoBaseURL", base, err)
			}
		})
	}
	if _, err := mail.Wrap(mem, "", "http://localhost"); !errors.Is(err, mail.ErrNoFrom) {
		t.Errorf("no From gave %v", err)
	}
	if _, err := mail.New(mail.Config{From: "a@b.test", BaseURL: "http://x"}); !errors.Is(err, mail.ErrNoAddr) {
		t.Errorf("no Addr gave %v", err)
	}
}

func TestAVerificationMailCarriesAUsableLink(t *testing.T) {
	m, mem := mailer(t)
	if err := m.Verify(context.Background(), "sam@example.com", "tok_123"); err != nil {
		t.Fatal(err)
	}
	msg, ok := mem.Last()
	if !ok {
		t.Fatal("nothing was sent")
	}
	if msg.To != "sam@example.com" || msg.Subject == "" {
		t.Errorf("%+v", msg)
	}

	// Assert on the LINK, not on a nil error. A test that only checks Send
	// returned nil passes against a mailer that sends a broken link.
	link := extract(t, msg.Body)
	u, err := url.Parse(link)
	if err != nil {
		t.Fatalf("unparseable link %q: %v", link, err)
	}
	if u.Host != "localhost:7010" || u.Path != "/verify" {
		t.Errorf("link points at %s%s", u.Host, u.Path)
	}
	if u.Query().Get("token") != "tok_123" {
		t.Errorf("token is %q", u.Query().Get("token"))
	}
	// Somebody who did not ask must be told that ignoring it is safe.
	if !strings.Contains(msg.Body, "ignore") {
		t.Error("the body does not tell an unexpecting recipient what to do")
	}
}

func TestAResetMailSaysNothingHasChangedYet(t *testing.T) {
	m, mem := mailer(t)
	if err := m.Reset(context.Background(), "sam@example.com", "tok_456"); err != nil {
		t.Fatal(err)
	}
	msg, _ := mem.Last()
	u, err := url.Parse(extract(t, msg.Body))
	if err != nil {
		t.Fatal(err)
	}
	if u.Path != "/reset" || u.Query().Get("token") != "tok_456" {
		t.Errorf("link %s", u)
	}
	if !strings.Contains(msg.Body, "still works") {
		t.Error("a reset mail that does not say the old password still works invites a panic")
	}
}

// Every message that carries a link builds it in one place, so there is one
// place the host can be wrong.
func TestLinksAreBuiltAgainstTheBaseAndNeverConcatenated(t *testing.T) {
	mem := mail.NewMemory()
	m, err := mail.Wrap(mem, "a@b.test", "https://overwatch.example/app/")
	if err != nil {
		t.Fatal(err)
	}
	got := m.Link("/verify", map[string]string{"token": "a b&c"})
	want := "https://overwatch.example/app/verify?token=a+b%26c"
	if got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
}

// Mailpit accepts everything and delivers nothing. Skips without it, like every
// other infrastructure test here.
func TestAgainstMailpit(t *testing.T) {
	addr := os.Getenv("MAIL_ADDR")
	if addr == "" {
		t.Skip("MAIL_ADDR is unset — run `make infra-up` in overwatch-infra")
	}
	m, err := mail.New(mail.Config{
		Addr: addr, From: "Overwatch <no-reply@overwatch.test>", BaseURL: "http://localhost:7010",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Verify(context.Background(), "sam@example.com", "tok_live"); err != nil {
		t.Fatalf("mailpit refused the message: %v", err)
	}
}

func extract(t *testing.T, body string) string {
	t.Helper()
	for _, f := range strings.Fields(body) {
		if strings.HasPrefix(f, "http://") || strings.HasPrefix(f, "https://") {
			return f
		}
	}
	t.Fatalf("no link in:\n%s", body)
	return ""
}
