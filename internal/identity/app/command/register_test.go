package command_test

import (
	"context"
	"crypto/rand"
	"testing"

	"github.com/0xsj/overwatch-backend/internal/identity/app/command"
	"github.com/0xsj/overwatch-backend/internal/identity/domain"
	"github.com/0xsj/overwatch-backend/internal/identity/infra/memory"
	"github.com/0xsj/overwatch-backend/pkg/clock"
	"github.com/0xsj/overwatch-backend/pkg/crypto"
	"github.com/0xsj/overwatch-backend/pkg/errors"
	"github.com/0xsj/overwatch-backend/pkg/events"
	"github.com/0xsj/overwatch-backend/pkg/id"
	"github.com/0xsj/overwatch-backend/pkg/secret"
)

var cheap = crypto.Params{Memory: 64, Iterations: 1, Parallelism: 1, SaltLength: 16, KeyLength: 32}

type spy struct {
	events []events.Event
	fail   error
}

func (s *spy) Publish(_ context.Context, evs ...events.Event) error {
	if s.fail != nil {
		return s.fail
	}
	s.events = append(s.events, evs...)
	return nil
}

func harness(t *testing.T) (*command.Registrar, *memory.Store, *spy) {
	t.Helper()
	clk := clock.System{}
	ids := id.NewV7(clk, rand.Reader)
	store := memory.New()
	pub := &spy{}
	return command.NewRegistrar(store, store, pub,
		crypto.NewHasher(cheap, rand.Reader), ids, clk), store, pub
}

func TestRegistrationWritesAnAccountAPasswordAndProvisionsTenancy(t *testing.T) {
	r, store, _ := harness(t)
	ctx := context.Background()

	got, err := r.Register(ctx, command.Registration{
		Email:    "Sam@Example.COM",
		Password: secret.New("a passphrase nobody guesses"),
		Name:     "Sam Lee",
	})
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	if got.Account.Email != "sam@example.com" || got.Account.Status != domain.StatusPending {
		t.Errorf("account %+v — a new account is pending until verified", got.Account)
	}

	cred, err := store.LivePasswordFor(ctx, got.Account.ID)
	if err != nil {
		t.Fatalf("password: %v", err)
	}
	if cred.Hash == "a passphrase nobody guesses" {
		t.Fatal("the plaintext was stored")
	}
}

// decisions/0014: the person chose to register. Nobody chose to have an org, so
// this package emits one event and the provisioner emits its own.
func TestRegistrationEmitsOneDecisionAndNothingElse(t *testing.T) {
	r, _, pub := harness(t)
	if _, err := r.Register(context.Background(), command.Registration{
		Email: "sam@example.com", Password: secret.New("a passphrase nobody guesses"),
	}); err != nil {
		t.Fatal(err)
	}
	if len(pub.events) != 1 {
		t.Fatalf("published %d events, want 1 — org and workspace provision themselves from it", len(pub.events))
	}
	e := pub.events[0]
	if e.Name != domain.EventAccountCreated || !e.Decision {
		t.Errorf("event %q Decision=%v", e.Name, e.Decision)
	}
	if e.SubjectKind() != "account" || e.SubjectID() == "" {
		t.Errorf("subject %q", e.Subject)
	}
	// A self-signup is unauthenticated. The trail says so rather than
	// attributing the act to the account it just created.
	if !e.Provenance.Actor().IsZero() {
		t.Errorf("names actor %q on an unauthenticated request", e.Provenance.Actor())
	}
}

// The reason this command exists rather than three calls at the root: no foreign
// key crosses a schema boundary, so nothing in the database prevents an account
// with no tenancy. The transaction is that guarantee, and the memory adapter
// implements it by snapshot and restore so both modes agree.
func TestAFailedPublishLeavesNoAccountBehind(t *testing.T) {
	r, store, pub := harness(t)
	ctx := context.Background()
	pub.fail = errors.New(errors.Unavailable, "the outbox is down")

	if _, err := r.Register(ctx, command.Registration{
		Email: "sam@example.com", Password: secret.New("a passphrase nobody guesses"),
	}); err == nil {
		t.Fatal("registration succeeded with a failing publisher")
	}
	if _, err := store.AccountByEmail(ctx, "sam@example.com"); !errors.Is(err, domain.ErrAccountNotFound) {
		t.Errorf("the account survived a rolled-back registration: %v", err)
	}
}

func TestASecondRegistrationOfTheSameAddressIsRefused(t *testing.T) {
	r, _, _ := harness(t)
	ctx := context.Background()
	in := command.Registration{Email: "sam@example.com", Password: secret.New("a passphrase nobody guesses")}

	if _, err := r.Register(ctx, in); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Register(ctx, in); !errors.Is(err, domain.ErrAccountExists) {
		t.Fatalf("the second registration gave %v, want ErrAccountExists", err)
	}
}

func TestThePolicyIsAppliedBeforeAnythingIsWritten(t *testing.T) {
	r, store, _ := harness(t)
	ctx := context.Background()

	for name, in := range map[string]command.Registration{
		"too short":      {Email: "a@example.com", Password: secret.New("short")},
		"empty":          {Email: "a@example.com", Password: secret.New("")},
		"not an address": {Email: "nope", Password: secret.New("a passphrase nobody guesses")},
		"no address":     {Email: "", Password: secret.New("a passphrase nobody guesses")},
		"no domain dot":  {Email: "a@example", Password: secret.New("a passphrase nobody guesses")},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := r.Register(ctx, in); err == nil {
				t.Fatal("accepted")
			} else if !errors.IsKind(err, errors.Invalid) {
				t.Errorf("refused as %v, want Invalid", err)
			}
		})
	}
	if _, err := store.AccountByEmail(ctx, "a@example.com"); !errors.Is(err, domain.ErrAccountNotFound) {
		t.Error("a refused registration wrote an account")
	}
}

func TestALongPassphraseIsAcceptedBecauseCompositionRulesAreNotAPolicy(t *testing.T) {
	r, _, _ := harness(t)
	long := "correct horse battery staple and then some more words for good measure"
	if _, err := r.Register(context.Background(), command.Registration{
		Email: "sam@example.com", Password: secret.New(long),
	}); err != nil {
		t.Fatalf("a long all-lowercase passphrase was refused: %v", err)
	}
}
