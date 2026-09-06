package command_test

import (
	"context"
	"crypto/rand"
	"testing"

	"github.com/0xsj/overwatch-backend/internal/workspace/app/command"
	"github.com/0xsj/overwatch-backend/internal/workspace/domain"
	"github.com/0xsj/overwatch-backend/internal/workspace/infra/memory"
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

func TestAnUnnamedWorkspaceGetsTheDefaultRatherThanABlank(t *testing.T) {
	s, store, _, ids := harness(t)
	ctx := context.Background()

	got, err := s.Provision(ctx, ids.NewID(), "")
	if err != nil {
		t.Fatalf("provision: %v", err)
	}
	if got.Name != command.DefaultName {
		t.Errorf("name %q, want %q — a workspace must never render blank", got.Name, command.DefaultName)
	}
	if _, err := store.ByID(ctx, got.ID); err != nil {
		t.Errorf("not written: %v", err)
	}
}

// provenance.Tenant means the workspace — decisions/0005. The created event is
// published under a provenance tenanted to the row it just made, which is what
// lets audit derive a `workspace` scope rather than filing a workspace's birth
// under `system`.
func TestTheCreatedEventIsTenantedToTheWorkspaceItMade(t *testing.T) {
	s, _, pub, ids := harness(t)
	got, err := s.Provision(context.Background(), ids.NewID(), "CTF1")
	if err != nil {
		t.Fatal(err)
	}
	if len(pub.events) != 1 {
		t.Fatalf("published %d events, want 1", len(pub.events))
	}
	e := pub.events[0]
	if e.Decision {
		t.Error("provisioning a workspace was recorded as somebody's decision")
	}
	if e.Provenance.Tenant() != got.ID.String() {
		t.Errorf("tenant %q, want the workspace %q", e.Provenance.Tenant(), got.ID)
	}
	if e.SubjectKind() != domain.SubjectKind || e.SubjectID() != got.ID.String() {
		t.Errorf("subject %q", e.Subject)
	}
}

func TestTwoLiveWorkspacesInOneOrgCannotShareANameEvenByCase(t *testing.T) {
	s, _, _, ids := harness(t)
	ctx := context.Background()
	org := ids.NewID()

	if _, err := s.Provision(ctx, org, "Acme Q3"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Provision(ctx, org, "acme q3"); !errors.Is(err, domain.ErrNameTaken) {
		t.Errorf("a case variant was accepted: %v — the Postgres index is on lower(name)", err)
	}
	// A different org may use it freely. That is the wall.
	if _, err := s.Provision(ctx, ids.NewID(), "Acme Q3"); err != nil {
		t.Errorf("another org was blocked by this org's name: %v", err)
	}
}

func TestAFailedPublishRollsBackWhenTheCallerHoldsTheTransaction(t *testing.T) {
	s, store, pub, ids := harness(t)
	ctx := context.Background()
	org := ids.NewID()
	pub.fail = errors.New(errors.Unavailable, "the outbox is down")

	err := store.InTx(ctx, func(ctx context.Context) error {
		_, err := s.Provision(ctx, org, "CTF1")
		return err
	})
	if err == nil {
		t.Fatal("provisioning succeeded with a failing publisher")
	}
	live, err := store.ForOrg(ctx, org)
	if err != nil {
		t.Fatal(err)
	}
	if len(live) != 0 {
		t.Errorf("%d workspaces survived the rollback", len(live))
	}
}
