package command_test

import (
	"context"
	"crypto/rand"
	"testing"

	"github.com/0xsj/overwatch-backend/internal/org/app/command"
	"github.com/0xsj/overwatch-backend/internal/org/domain"
	"github.com/0xsj/overwatch-backend/internal/org/infra/memory"
	"github.com/0xsj/overwatch-backend/pkg/clock"
	"github.com/0xsj/overwatch-backend/pkg/errors"
	"github.com/0xsj/overwatch-backend/pkg/events"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

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

func harness(t *testing.T) (*command.Service, *memory.Store, *spy, *id.V7) {
	t.Helper()
	clk := clock.System{}
	ids := id.NewV7(clk, rand.Reader)
	store := memory.New()
	pub := &spy{}
	return command.NewService(store, pub, ids, clk), store, pub, ids
}

func TestProvisioningMakesAnOrgAndItsFoundingOwner(t *testing.T) {
	s, store, pub, ids := harness(t)
	ctx := context.Background()
	owner := ids.NewID()

	got, err := s.Provision(ctx, owner, "Sam Lee", ids.NewID())
	if err != nil {
		t.Fatalf("provision: %v", err)
	}
	if got.Org.Name != "Sam Lee" || got.Member.AccountID != owner || got.Member.Role != domain.RoleOwner {
		t.Errorf("provisioned %+v", got)
	}
	if _, err := store.LiveMemberFor(ctx, got.Org.ID, owner); err != nil {
		t.Errorf("the membership was not written: %v", err)
	}

	// decisions/0014: nobody CHOSE to have an org. It is provisioning, and a
	// ledger that records automatic consequences as decisions dilutes itself.
	if len(pub.events) != 1 {
		t.Fatalf("published %d events, want 1", len(pub.events))
	}
	e := pub.events[0]
	if e.Name != domain.EventOrgCreated || e.Decision {
		t.Errorf("event %q Decision=%v, want false", e.Name, e.Decision)
	}
	if e.SubjectKind() != domain.SubjectKind || e.SubjectID() != got.Org.ID.String() {
		t.Errorf("subject %q", e.Subject)
	}
}

func TestTheSameAccountCannotBeProvisionedIntoOneOrgTwice(t *testing.T) {
	s, store, _, ids := harness(t)
	ctx := context.Background()
	owner := ids.NewID()

	first, err := s.Provision(ctx, owner, "Sam Lee", ids.NewID())
	if err != nil {
		t.Fatal(err)
	}
	// Two orgs may share a name — a personal org is named after its owner, and
	// two people called Sam Lee are two orgs.
	if _, err := s.Provision(ctx, ids.NewID(), "Sam Lee", ids.NewID()); err != nil {
		t.Fatalf("a second org of the same name was refused: %v", err)
	}
	// But one account cannot hold two live memberships of one org.
	dup, err := domain.NewMember(ids.NewID(), first.Org.ID, owner, domain.RoleOwner, first.Org.CreatedAt)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.AddMember(ctx, dup); !errors.Is(err, domain.ErrMemberExists) {
		t.Errorf("the memory adapter accepted what the index refuses: %v", err)
	}
}

func TestAFailedPublishRollsBackWhenTheCallerHoldsTheTransaction(t *testing.T) {
	s, store, pub, ids := harness(t)
	ctx := context.Background()
	owner := ids.NewID()
	pub.fail = errors.New(errors.Unavailable, "the outbox is down")

	// This package opens no transaction: it runs inside somebody else's. The
	// rollback is therefore the caller's, and this is what that looks like.
	err := store.InTx(ctx, func(ctx context.Context) error {
		_, err := s.Provision(ctx, owner, "Sam Lee", ids.NewID())
		return err
	})
	if err == nil {
		t.Fatal("provisioning succeeded with a failing publisher")
	}
	if _, err := store.LiveMemberFor(ctx, id.Nil, owner); err == nil {
		t.Error("a membership survived the rollback")
	}
}

func TestAnOrgNeedsAName(t *testing.T) {
	s, _, _, ids := harness(t)
	if _, err := s.Provision(context.Background(), ids.NewID(), "   ", ids.NewID()); !errors.Is(err, domain.ErrNameRequired) {
		t.Errorf("an unnamed org was accepted: %v", err)
	}
}
