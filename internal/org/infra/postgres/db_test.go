package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/0xsj/overwatch-backend/internal/org/domain"
	orgpg "github.com/0xsj/overwatch-backend/internal/org/infra/postgres"
	"github.com/0xsj/overwatch-backend/pkg/errors"
	"github.com/0xsj/overwatch-backend/pkg/id"
	"github.com/0xsj/overwatch-backend/pkg/postgres"
	"github.com/0xsj/overwatch-backend/pkg/testx"
)

var at = time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)

func store(t *testing.T) (*orgpg.Store, *postgres.Pool) {
	t.Helper()
	p := testx.Postgres(t, testx.Schema{Name: orgpg.Schema, Migrations: orgpg.Migrations})
	return orgpg.NewStore(p), p
}

func ids() *id.Sequence { return id.NewSequence(at) }

func personal(t *testing.T, s *orgpg.Store, m *id.Sequence, name string) (domain.Org, domain.Member, id.ID) {
	t.Helper()
	ctx := context.Background()
	account := m.NewID()
	o, err := domain.NewOrg(m.NewID(), name, at)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.CreateOrg(ctx, o); err != nil {
		t.Fatalf("create org: %v", err)
	}
	member, err := domain.NewMember(m.NewID(), o.ID, account, domain.RoleOwner, at)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.AddMember(ctx, member); err != nil {
		t.Fatalf("add member: %v", err)
	}
	return o, member, account
}

func TestAPersonalOrgIsAnOrgWithOneOwner(t *testing.T) {
	s, _ := store(t)
	ctx := context.Background()
	m := ids()
	o, _, account := personal(t, s, m, "Sam Lee")

	back, err := s.OrgByID(ctx, o.ID)
	if err != nil || back.Name != "Sam Lee" || back.Version != 1 {
		t.Fatalf("read back %+v: %v", back, err)
	}

	mine, err := s.OrgsForAccount(ctx, account)
	if err != nil || len(mine) != 1 || mine[0].ID != o.ID {
		t.Fatalf("orgs for the account: %+v %v", mine, err)
	}

	member, err := s.LiveMemberFor(ctx, o.ID, account)
	if err != nil || member.Role != domain.RoleOwner || member.Archived() {
		t.Fatalf("membership %+v: %v", member, err)
	}
}

// Two people called Sam Lee are two orgs. A globally unique name would fail the
// second signup for a reason nobody could act on.
func TestTwoOrgsMayShareAName(t *testing.T) {
	s, _ := store(t)
	m := ids()
	personal(t, s, m, "Sam Lee")
	personal(t, s, m, "Sam Lee")
}

func TestAnAccountCannotJoinTheSameOrgTwiceButMayRejoinAfterLeaving(t *testing.T) {
	s, _ := store(t)
	ctx := context.Background()
	m := ids()
	o, first, account := personal(t, s, m, "Vertex Labs Security")

	dup, err := domain.NewMember(m.NewID(), o.ID, account, domain.RoleOwner, at)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.AddMember(ctx, dup); !errors.Is(err, domain.ErrMemberExists) {
		t.Fatalf("a second live membership gave %v, want ErrMemberExists", err)
	}

	// A second owner, so archiving the first is not the last-owner case.
	second, err := domain.NewMember(m.NewID(), o.ID, m.NewID(), domain.RoleOwner, at)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.AddMember(ctx, second); err != nil {
		t.Fatal(err)
	}

	archived, err := first.Archive(at.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SaveMember(ctx, archived); err != nil {
		t.Fatalf("archive: %v", err)
	}
	if _, err := s.LiveMemberFor(ctx, o.ID, account); !errors.Is(err, domain.ErrMemberNotFound) {
		t.Errorf("an archived membership still reads as live: %v", err)
	}
	// The row survives, because every attribution made under it points at it.
	if _, err := s.MemberByID(ctx, first.ID); err != nil {
		t.Errorf("the archived row is gone: %v", err)
	}
	if err := s.AddMember(ctx, dup); err != nil {
		t.Errorf("rejoining after leaving was refused: %v", err)
	}
}

// The last-owner rule USED TO LIVE HERE and this test asserted it here. It
// moved to the command on 2026-09-07 — decisions/0026 — because the adapter sees
// one row and the rule is about a SET: it covered archiving and not demotion,
// and its count did not lock, so two concurrent demotions of two different
// owners both passed it.
//
// What the store owes the command is the LOCKED READ, and that is what this
// asserts now. The invariant itself is asserted where it is enforced, in
// root/members_e2e_test.go.
func TestTheStoreLendsTheCommandALockedOwnerRead(t *testing.T) {
	s, _ := store(t)
	ctx := context.Background()
	m := ids()
	_, only, _ := personal(t, s, m, "Solo")

	owners, err := s.LockLiveOwners(ctx, only.OrgID)
	if err != nil {
		t.Fatal(err)
	}
	// ACCOUNT ids, not member ids. The command compares these against the
	// account it is about to demote, and a member id would never match one.
	if len(owners) != 1 || owners[0] != only.AccountID {
		t.Fatalf("owners: %v, want the founding account %s", owners, only.AccountID)
	}

	// The store no longer refuses. That is deliberate and is the whole content
	// of the move: an adapter that guards is an adapter every other write path
	// has to remember to go through.
	archived, err := only.Archive(at.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SaveMember(ctx, archived); err != nil {
		t.Fatalf("the store refused a write the command is responsible for: %v", err)
	}
	if owners, err = s.LockLiveOwners(ctx, only.OrgID); err != nil || len(owners) != 0 {
		t.Errorf("after archiving: %v %v", owners, err)
	}
}

func TestAStaleSaveIsRefusedRatherThanLost(t *testing.T) {
	s, _ := store(t)
	ctx := context.Background()
	m := ids()
	o, _, _ := personal(t, s, m, "Vertex")

	mine, _ := o.Rename("Mine", at.Add(time.Minute))
	theirs, _ := o.Rename("Theirs", at.Add(time.Minute))
	if err := s.SaveOrg(ctx, mine); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveOrg(ctx, theirs); !errors.Is(err, domain.ErrStaleWrite) {
		t.Fatalf("the losing write gave %v, want ErrStaleWrite", err)
	}
	back, _ := s.OrgByID(ctx, o.ID)
	if back.Name != "Mine" {
		t.Errorf("name is %q — the losing write was applied", back.Name)
	}
}

// account_id names a row in identity's schema and is deliberately not a
// reference. This asserts the cost rather than the guarantee: the database
// accepts a member whose account does not exist, and the registration
// transaction is the only thing that stops it.
func TestAMemberNamingANonexistentAccountIsAcceptedAndThatIsTheTrade(t *testing.T) {
	s, _ := store(t)
	ctx := context.Background()
	m := ids()
	o, _, _ := personal(t, s, m, "Vertex")

	ghost, err := domain.NewMember(m.NewID(), o.ID, m.NewID(), domain.RoleOwner, at)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.AddMember(ctx, ghost); err != nil {
		t.Fatalf("no foreign key crosses out of this schema, so this must succeed: %v", err)
	}
}
