package postgres_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/0xsj/overwatch-backend/internal/identity/domain"
	identitypg "github.com/0xsj/overwatch-backend/internal/identity/infra/postgres"
	"github.com/0xsj/overwatch-backend/pkg/errors"
	"github.com/0xsj/overwatch-backend/pkg/id"
	"github.com/0xsj/overwatch-backend/pkg/postgres"
)

var at = time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)

func store(t *testing.T) (*identitypg.Store, *postgres.Pool) {
	t.Helper()
	dsn := os.Getenv("OVERWATCH_TEST_DSN")
	if dsn == "" {
		t.Skip("OVERWATCH_TEST_DSN is unset — run `make test-db`")
	}
	ctx := context.Background()
	p, err := postgres.Open(ctx, postgres.Config{DSN: dsn})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(p.Close)

	if _, err := p.DB(ctx).Exec(ctx, "drop schema if exists identity cascade"); err != nil {
		t.Fatalf("drop schema: %v", err)
	}
	if _, err := postgres.Migrate(ctx, p, identitypg.Migrations, postgres.InSchema(identitypg.Schema)); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return identitypg.NewStore(p), p
}

func newIDs() *id.Sequence { return id.NewSequence(at) }

func liveAccount(t *testing.T, s *identitypg.Store, ids *id.Sequence, email string) domain.Account {
	t.Helper()
	e, err := domain.NewEmail(email)
	if err != nil {
		t.Fatalf("email: %v", err)
	}
	a, err := domain.NewAccount(ids.NewID(), e, "Sam", at)
	if err != nil {
		t.Fatalf("account: %v", err)
	}
	if err := s.CreateAccount(context.Background(), a); err != nil {
		t.Fatalf("create: %v", err)
	}
	return a
}

func TestTheMigrationPutsItsLedgerInIdentitysOwnSchema(t *testing.T) {
	_, p := store(t)
	ctx := context.Background()
	var n int
	if err := p.DB(ctx).QueryRow(ctx,
		"select count(*) from identity.schema_migrations").Scan(&n); err != nil {
		t.Fatalf("identity.schema_migrations: %v", err)
	}
	if n != len(identitypg.Migrations) {
		t.Errorf("ledger has %d rows, want %d", n, len(identitypg.Migrations))
	}
}

func TestAnAccountRoundTripsByIDAndByEmail(t *testing.T) {
	s, _ := store(t)
	ctx := context.Background()
	want := liveAccount(t, s, newIDs(), "Sam@Example.COM")

	got, err := s.AccountByID(ctx, want.ID)
	if err != nil {
		t.Fatalf("by id: %v", err)
	}
	if got.Email != "sam@example.com" || got.Status != domain.StatusPending || got.Version != 1 {
		t.Errorf("read back %+v", got)
	}
	if !got.CreatedAt.Equal(at) {
		t.Errorf("created_at %v, want %v", got.CreatedAt, at)
	}

	byEmail, err := s.AccountByEmail(ctx, "sam@example.com")
	if err != nil || byEmail.ID != want.ID {
		t.Errorf("by email: %+v %v", byEmail, err)
	}
}

func TestTwoAccountsCannotShareALiveEmailAndAnArchivedOneReleasesIt(t *testing.T) {
	s, _ := store(t)
	ctx := context.Background()
	ids := newIDs()
	first := liveAccount(t, s, ids, "dup@example.com")

	e, _ := domain.NewEmail("dup@example.com")
	second, _ := domain.NewAccount(ids.NewID(), e, "Other", at)
	if err := s.CreateAccount(ctx, second); !errors.Is(err, domain.ErrAccountExists) {
		t.Fatalf("second insert gave %v, want ErrAccountExists", err)
	}

	archived, err := first.Archive(at.Add(time.Hour))
	if err != nil {
		t.Fatalf("archive: %v", err)
	}
	if err := s.SaveAccount(ctx, archived); err != nil {
		t.Fatalf("save archived: %v", err)
	}
	if err := s.CreateAccount(ctx, second); err != nil {
		t.Fatalf("after archiving the first, the address is free: %v", err)
	}
	if _, err := s.AccountByEmail(ctx, "dup@example.com"); err != nil {
		t.Fatalf("by email finds the live one: %v", err)
	}
}

func TestASaveAgainstAStaleVersionIsRefusedRatherThanLost(t *testing.T) {
	s, _ := store(t)
	ctx := context.Background()
	a := liveAccount(t, s, newIDs(), "stale@example.com")

	mine, _ := a.Rename("Mine", at.Add(time.Minute))
	theirs, _ := a.Rename("Theirs", at.Add(time.Minute))

	if err := s.SaveAccount(ctx, mine); err != nil {
		t.Fatalf("first save: %v", err)
	}
	if err := s.SaveAccount(ctx, theirs); !errors.Is(err, domain.ErrStaleWrite) {
		t.Fatalf("second save gave %v, want ErrStaleWrite", err)
	}
	got, _ := s.AccountByID(ctx, a.ID)
	if got.Name != "Mine" {
		t.Errorf("name is %q — the losing write was applied", got.Name)
	}
}

func TestAMissingAccountIsNotFoundAndNotAStaleWrite(t *testing.T) {
	s, _ := store(t)
	ctx := context.Background()
	ghost, _ := domain.NewAccount(newIDs().NewID(), "nobody@example.com", "", at)
	next, _ := ghost.Rename("x", at.Add(time.Minute))
	if err := s.SaveAccount(ctx, next); !errors.Is(err, domain.ErrAccountNotFound) {
		t.Fatalf("gave %v, want ErrAccountNotFound", err)
	}
}

func TestAnAccountCanHoldOneLivePasswordAndManyKeys(t *testing.T) {
	s, _ := store(t)
	ctx := context.Background()
	ids := newIDs()
	a := liveAccount(t, s, ids, "creds@example.com")

	pw, err := domain.NewPassword(ids.NewID(), a.ID, "$argon2id$one", at)
	if err != nil {
		t.Fatalf("password: %v", err)
	}
	if err := s.CreateCredential(ctx, pw); err != nil {
		t.Fatalf("insert password: %v", err)
	}

	second, _ := domain.NewPassword(ids.NewID(), a.ID, "$argon2id$two", at)
	if err := s.CreateCredential(ctx, second); err == nil {
		t.Error("a second live password was accepted")
	}

	for _, name := range []string{"laptop", "ci"} {
		k, err := domain.NewAPIKey(ids.NewID(), a.ID, "$sha256$"+name, name, time.Time{}, at)
		if err != nil {
			t.Fatalf("key %s: %v", name, err)
		}
		if err := s.CreateCredential(ctx, k); err != nil {
			t.Fatalf("insert key %s: %v", name, err)
		}
	}

	all, err := s.CredentialsFor(ctx, a.ID)
	if err != nil || len(all) != 3 {
		t.Fatalf("listed %d credentials: %v", len(all), err)
	}

	live, err := s.LivePasswordFor(ctx, a.ID)
	if err != nil || live.Hash != "$argon2id$one" {
		t.Fatalf("live password %+v: %v", live, err)
	}
}

func TestRevokingThePasswordFreesTheSlotAndKeepsTheOldRow(t *testing.T) {
	s, _ := store(t)
	ctx := context.Background()
	ids := newIDs()
	a := liveAccount(t, s, ids, "rotate@example.com")

	pw, _ := domain.NewPassword(ids.NewID(), a.ID, "$argon2id$old", at)
	if err := s.CreateCredential(ctx, pw); err != nil {
		t.Fatalf("insert: %v", err)
	}
	revoked, err := pw.Revoke(at.Add(time.Hour))
	if err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if err := s.SaveCredential(ctx, revoked); err != nil {
		t.Fatalf("save revoked: %v", err)
	}

	fresh, _ := domain.NewPassword(ids.NewID(), a.ID, "$argon2id$new", at.Add(time.Hour))
	if err := s.CreateCredential(ctx, fresh); err != nil {
		t.Fatalf("the slot is free once the old one is revoked: %v", err)
	}
	if _, err := s.CredentialByID(ctx, pw.ID); err != nil {
		t.Errorf("the revoked row is gone: %v", err)
	}
	if _, err := s.LivePasswordFor(ctx, a.ID); err != nil {
		t.Errorf("live password after rotation: %v", err)
	}
}

// There is no DeleteAccount, deliberately — decisions/0005 archives and never
// deletes. This asserts the half that holds when somebody reaches the database
// anyway: ON DELETE RESTRICT means the evidence cannot be orphaned by a DELETE
// that the Go API refuses to offer.
func TestTheDatabaseRefusesToDeleteAnAccountThatSomethingStillNames(t *testing.T) {
	s, p := store(t)
	ctx := context.Background()
	ids := newIDs()
	a := liveAccount(t, s, ids, "fk@example.com")
	pw, _ := domain.NewPassword(ids.NewID(), a.ID, "$argon2id$x", at)
	if err := s.CreateCredential(ctx, pw); err != nil {
		t.Fatalf("insert: %v", err)
	}

	_, err := p.DB(ctx).Exec(ctx, "delete from identity.account where id = $1", a.ID)
	if err == nil {
		t.Fatal("the account was deleted out from under its credential")
	}
	if !errors.IsKind(postgres.Translate(ctx, err, "delete"), errors.Conflict) {
		t.Errorf("refusal came back as %v, want Conflict — 23001 is restrict_violation", err)
	}
}

func TestASessionIsFoundByItsHashAndEndsForEveryoneAtOnce(t *testing.T) {
	s, _ := store(t)
	ctx := context.Background()
	ids := newIDs()
	a := liveAccount(t, s, ids, "sessions@example.com")
	expires := at.Add(24 * time.Hour)

	for _, h := range []string{"$sha256$a", "$sha256$b"} {
		sn, err := domain.NewSession(ids.NewID(), a.ID, h, "curl/8", "203.0.113.9", at, expires)
		if err != nil {
			t.Fatalf("session: %v", err)
		}
		if err := s.CreateSession(ctx, sn); err != nil {
			t.Fatalf("insert session: %v", err)
		}
	}

	got, err := s.SessionByHash(ctx, "$sha256$a")
	if err != nil || got.Address != "203.0.113.9" {
		t.Fatalf("by hash %+v: %v", got, err)
	}
	if !got.Live(at.Add(time.Hour)) {
		t.Error("a fresh session reads as not live")
	}

	n, err := s.EndSessionsFor(ctx, a.ID, at.Add(time.Hour))
	if err != nil || n != 2 {
		t.Fatalf("ended %d sessions: %v", n, err)
	}
	again, err := s.SessionByHash(ctx, "$sha256$a")
	if err != nil {
		t.Fatalf("an ended session is still readable: %v", err)
	}
	if again.Live(at.Add(2 * time.Hour)) {
		t.Error("an ended session still reads as live")
	}
	if n, _ := s.EndSessionsFor(ctx, a.ID, at.Add(2*time.Hour)); n != 0 {
		t.Errorf("ending twice touched %d rows", n)
	}
}
