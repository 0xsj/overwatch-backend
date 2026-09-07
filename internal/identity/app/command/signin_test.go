package command_test

import (
	"context"
	"crypto/rand"
	"strings"
	"testing"
	"time"

	"github.com/0xsj/overwatch-backend/internal/identity/app/command"
	"github.com/0xsj/overwatch-backend/internal/identity/app/query"
	"github.com/0xsj/overwatch-backend/internal/identity/domain"
	"github.com/0xsj/overwatch-backend/internal/identity/infra/memory"
	"github.com/0xsj/overwatch-backend/pkg/clock"
	"github.com/0xsj/overwatch-backend/pkg/crypto"
	"github.com/0xsj/overwatch-backend/pkg/errors"
	"github.com/0xsj/overwatch-backend/pkg/id"
	"github.com/0xsj/overwatch-backend/pkg/secret"
)

const goodPassword = "a passphrase nobody guesses"

type authed struct {
	auth     *command.Authenticator
	sessions *query.Sessions
	store    *memory.Store
	pub      *spy
	hasher   *crypto.Hasher
}

func signedInHarness(t *testing.T, params crypto.Params) authed {
	t.Helper()
	clk := clock.System{}
	ids := id.NewV7(clk, rand.Reader)
	store := memory.New()
	pub := &spy{}
	hasher := crypto.NewHasher(params, rand.Reader)

	registrar := command.NewRegistrar(store, store, pub, hasher, ids, clk)
	if _, err := registrar.Register(context.Background(), command.Registration{
		Email: "sam@example.com", Password: secret.New(goodPassword), Name: "Sam",
	}); err != nil {
		t.Fatal(err)
	}
	pub.events = nil

	return authed{
		auth:     command.NewAuthenticator(store, pub, hasher, crypto.NewMinter(rand.Reader), ids, clk, 0),
		sessions: query.NewSessions(store, clk),
		store:    store,
		pub:      pub,
		hasher:   hasher,
	}
}

func TestSigningInReturnsATokenThatAuthenticates(t *testing.T) {
	h := signedInHarness(t, cheap)
	ctx := context.Background()

	in, err := h.auth.SignIn(ctx, command.Credentials{
		Email: "Sam@Example.COM", Password: secret.New(goodPassword),
		UserAgent: "curl/8", Address: "203.0.113.9",
	})
	if err != nil {
		t.Fatalf("sign in: %v", err)
	}
	if in.Account.Email != "sam@example.com" {
		t.Errorf("account %+v", in.Account)
	}
	// decisions/0018: a pending account signs in. Verification gates capability,
	// not identity.
	if in.Account.Status != domain.StatusPending {
		t.Errorf("status %v — this test is no longer about a pending account", in.Account.Status)
	}

	token := in.Token.Reveal()
	if token == "" {
		t.Fatal("no token")
	}
	// Only the hash is stored. A stolen database yields nothing presentable.
	stored, err := h.store.SessionByHash(ctx, crypto.HashToken(token))
	if err != nil {
		t.Fatalf("the session was not stored under the token's hash: %v", err)
	}
	if stored.Hash == token {
		t.Fatal("the plaintext token was stored")
	}
	if stored.UserAgent != "curl/8" || stored.Address != "203.0.113.9" {
		t.Errorf("a session a person cannot recognise is a session they cannot revoke: %+v", stored)
	}

	caller, err := h.sessions.Authenticate(ctx, token)
	if err != nil {
		t.Fatalf("the token did not authenticate: %v", err)
	}
	if caller.AccountID != in.Account.ID || caller.SessionID != in.Session.ID {
		t.Errorf("caller %+v", caller)
	}
	if caller.Verified() {
		t.Error("a pending caller reported as verified — the capability gate would let it act")
	}
}

// Three different truths, one answer. Any endpoint that distinguishes them is a
// user directory.
func TestEveryRejectionLooksTheSame(t *testing.T) {
	h := signedInHarness(t, cheap)
	ctx := context.Background()

	for name, in := range map[string]command.Credentials{
		"a wrong password":       {Email: "sam@example.com", Password: secret.New("not it at all")},
		"an unknown address":     {Email: "nobody@example.com", Password: secret.New(goodPassword)},
		"an unparseable address": {Email: "not-an-address", Password: secret.New(goodPassword)},
		"an empty password":      {Email: "sam@example.com", Password: secret.New("")},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := h.auth.SignIn(ctx, in)
			if !errors.Is(err, command.ErrRejected) {
				t.Fatalf("gave %v, want ErrRejected", err)
			}
			if errors.KindOf(err) != errors.Unauthenticated {
				t.Errorf("kind %v", errors.KindOf(err))
			}
			// The message must not name which half was wrong.
			msg := strings.ToLower(errors.Message(err))
			for _, leak := range []string{"no such", "not found", "unknown account", "wrong password"} {
				if strings.Contains(msg, leak) {
					t.Errorf("the message leaks which half failed: %q", msg)
				}
			}
		})
	}
}

// The timing half of the same defence: a caller that finds no account still does
// the work, or the endpoint is readable with a stopwatch.
func TestAnUnknownAddressCostsWhatAWrongPasswordCosts(t *testing.T) {
	// Real parameters, because the whole point is that the KDF dominates.
	h := signedInHarness(t, crypto.Default)
	ctx := context.Background()

	known := time.Now()
	_, _ = h.auth.SignIn(ctx, command.Credentials{
		Email: "sam@example.com", Password: secret.New("wrong")})
	knownTook := time.Since(known)

	unknown := time.Now()
	_, _ = h.auth.SignIn(ctx, command.Credentials{
		Email: "nobody@example.com", Password: secret.New("wrong")})
	unknownTook := time.Since(unknown)

	// A dummy verification is one argon2 run; skipping it makes the unknown case
	// orders of magnitude faster. An order of magnitude is the signal, so half
	// is a generous floor.
	if unknownTook < knownTook/2 {
		t.Errorf("unknown address took %v against %v for a known one — the dummy hash is not being spent",
			unknownTook, knownTook)
	}
}

func TestAnArchivedAccountCannotSignInEvenWithTheRightPassword(t *testing.T) {
	h := signedInHarness(t, cheap)
	ctx := context.Background()

	account, err := h.store.AccountByEmail(ctx, "sam@example.com")
	if err != nil {
		t.Fatal(err)
	}
	archived, err := account.Archive(time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err := h.store.SaveAccount(ctx, archived); err != nil {
		t.Fatal(err)
	}

	if _, err := h.auth.SignIn(ctx, command.Credentials{
		Email: "sam@example.com", Password: secret.New(goodPassword),
	}); !errors.Is(err, command.ErrRejected) {
		t.Fatalf("an archived account signed in: %v", err)
	}
}

// The one moment the plaintext is in hand. Deferring it freezes whatever
// parameters were chosen on the first day for the life of every account.
func TestAHashBelowPolicyIsRewrittenDuringSignIn(t *testing.T) {
	h := signedInHarness(t, cheap)
	ctx := context.Background()

	account, _ := h.store.AccountByEmail(ctx, "sam@example.com")
	before, err := h.store.LivePasswordFor(ctx, account.ID)
	if err != nil {
		t.Fatal(err)
	}

	stronger := cheap
	stronger.Iterations = cheap.Iterations + 1
	clk := clock.System{}
	ids := id.NewV7(clk, rand.Reader)
	strict := command.NewAuthenticator(h.store, h.pub, crypto.NewHasher(stronger, rand.Reader),
		crypto.NewMinter(rand.Reader), ids, clk, 0)

	if _, err := strict.SignIn(ctx, command.Credentials{
		Email: "sam@example.com", Password: secret.New(goodPassword),
	}); err != nil {
		t.Fatalf("a hash below policy did not verify: %v", err)
	}

	after, err := h.store.LivePasswordFor(ctx, account.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Hash == before.Hash {
		t.Fatal("the credential was not rehashed at the one moment the plaintext existed")
	}
	if after.Version != before.Version+1 {
		t.Errorf("version %d -> %d", before.Version, after.Version)
	}
	// And the new hash still authenticates.
	if _, err := strict.SignIn(ctx, command.Credentials{
		Email: "sam@example.com", Password: secret.New(goodPassword),
	}); err != nil {
		t.Fatalf("the rehashed credential does not verify: %v", err)
	}
}

func TestSigningOutStopsTheTokenAndIsIdempotent(t *testing.T) {
	h := signedInHarness(t, cheap)
	ctx := context.Background()

	in, err := h.auth.SignIn(ctx, command.Credentials{
		Email: "sam@example.com", Password: secret.New(goodPassword)})
	if err != nil {
		t.Fatal(err)
	}
	token := in.Token.Reveal()

	if err := h.auth.SignOut(ctx, token); err != nil {
		t.Fatal(err)
	}
	if _, err := h.sessions.Authenticate(ctx, token); !errors.Is(err, query.ErrNoSession) {
		t.Errorf("a revoked token still authenticates: %v", err)
	}
	// Signing out twice is the outcome the caller wanted, so it is not an error.
	if err := h.auth.SignOut(ctx, token); err != nil {
		t.Errorf("a second sign-out reported %v", err)
	}
	if err := h.auth.SignOut(ctx, "a token that never existed"); err != nil {
		t.Errorf("signing out an unknown token reported %v", err)
	}
}

// Unknown, revoked and expired are one answer: distinguishing them makes the
// endpoint an oracle for guessing tokens.
func TestEveryFailureToAuthenticateIsTheSameAnswer(t *testing.T) {
	h := signedInHarness(t, cheap)
	ctx := context.Background()

	for name, token := range map[string]string{
		"empty":       "",
		"nonsense":    "not-a-token",
		"well-formed": strings.Repeat("a", 43),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := h.sessions.Authenticate(ctx, token); !errors.Is(err, query.ErrNoSession) {
				t.Errorf("gave %v, want ErrNoSession", err)
			}
		})
	}
}

// An account archived AFTER a session was minted must stop working at once. The
// session's own expiry cannot see that.
func TestArchivingAnAccountStopsAnAlreadyIssuedToken(t *testing.T) {
	h := signedInHarness(t, cheap)
	ctx := context.Background()

	in, err := h.auth.SignIn(ctx, command.Credentials{
		Email: "sam@example.com", Password: secret.New(goodPassword)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.sessions.Authenticate(ctx, in.Token.Reveal()); err != nil {
		t.Fatal(err)
	}

	account, _ := h.store.AccountByEmail(ctx, "sam@example.com")
	archived, _ := account.Archive(time.Now())
	if err := h.store.SaveAccount(ctx, archived); err != nil {
		t.Fatal(err)
	}

	if _, err := h.sessions.Authenticate(ctx, in.Token.Reveal()); !errors.Is(err, query.ErrNoSession) {
		t.Errorf("an archived account's live session still authenticates: %v", err)
	}
}

func TestAnExpiredSessionStopsAuthenticating(t *testing.T) {
	h := signedInHarness(t, cheap)
	ctx := context.Background()
	clk := clock.System{}
	ids := id.NewV7(clk, rand.Reader)

	brief := command.NewAuthenticator(h.store, h.pub, h.hasher,
		crypto.NewMinter(rand.Reader), ids, clk, 40*time.Millisecond)
	in, err := brief.SignIn(ctx, command.Credentials{
		Email: "sam@example.com", Password: secret.New(goodPassword)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.sessions.Authenticate(ctx, in.Token.Reveal()); err != nil {
		t.Fatal(err)
	}
	time.Sleep(60 * time.Millisecond)
	if _, err := h.sessions.Authenticate(ctx, in.Token.Reveal()); !errors.Is(err, query.ErrNoSession) {
		t.Errorf("an expired session still authenticates: %v", err)
	}
}

func TestSigningInIsADecisionAndSaysWhoseSession(t *testing.T) {
	h := signedInHarness(t, cheap)
	ctx := context.Background()

	in, err := h.auth.SignIn(ctx, command.Credentials{
		Email: "sam@example.com", Password: secret.New(goodPassword)})
	if err != nil {
		t.Fatal(err)
	}
	if len(h.pub.events) != 1 {
		t.Fatalf("published %d events, want 1", len(h.pub.events))
	}
	e := h.pub.events[0]
	if e.Name != domain.EventSessionStarted || !e.Decision {
		t.Errorf("event %q decision=%v", e.Name, e.Decision)
	}
	// The subject is the ACCOUNT, not the session: "what happened to this
	// account" is the question a trail is asked.
	if e.SubjectKind() != "account" || e.SubjectID() != in.Account.ID.String() {
		t.Errorf("subject %q", e.Subject)
	}
}
