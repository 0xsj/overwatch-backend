package command_test

import (
	"context"
	"testing"
	"time"

	"github.com/0xsj/overwatch-backend/internal/org/app/command"
	orgquery "github.com/0xsj/overwatch-backend/internal/org/app/query"
	"github.com/0xsj/overwatch-backend/internal/org/domain"
	"github.com/0xsj/overwatch-backend/internal/org/infra/memory"
	"github.com/0xsj/overwatch-backend/pkg/clock"
	"github.com/0xsj/overwatch-backend/pkg/errors"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

type roleChangeStore struct {
	command.MemberRepository
	store *memory.Store
}

func (s roleChangeStore) LiveMemberFor(ctx context.Context, org, account id.ID) (domain.Member, error) {
	return s.store.LiveMemberFor(ctx, org, account)
}
func (s roleChangeStore) SaveMember(ctx context.Context, member domain.Member) error {
	return s.store.SaveMember(ctx, member)
}

func TestRoleChangeRollsBackWhenItsAuditEventFails(t *testing.T) {
	provision, store, pub, ids := harness(t)
	ctx := context.Background()
	owner, account := ids.NewID(), ids.NewID()
	firm, err := provision.Provision(ctx, owner, "Research", ids.NewID())
	if err != nil {
		t.Fatal(err)
	}
	member, err := domain.NewMember(ids.NewID(), firm.Org.ID, account, domain.RoleMember, time.Time{}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.AddMember(ctx, member); err != nil {
		t.Fatal(err)
	}
	pub.fail = errors.New(errors.Unavailable, "outbox unavailable")
	members := command.NewMembers(roleChangeStore{store: store}, pub, store, ids, clock.System{})
	if err := members.ChangeRole(ctx, owner, firm.Org.ID, account, domain.RoleAdmin, time.Time{}); !errors.Is(err, pub.fail) {
		t.Fatalf("publication failure should reach caller: %v", err)
	}
	held, err := store.LiveMemberFor(ctx, firm.Org.ID, account)
	if err != nil || held.Role != domain.RoleMember {
		t.Fatalf("role change survived failed audit: %+v, %v", held, err)
	}
}

func TestMembersCannotPromoteTheirOwnRole(t *testing.T) {
	for _, role := range []domain.Role{domain.RoleMember, domain.RoleGuest, domain.RoleClient} {
		t.Run(role.String(), func(t *testing.T) {
			provision, store, pub, ids := harness(t)
			ctx := context.Background()
			owner, account := ids.NewID(), ids.NewID()
			firm, err := provision.Provision(ctx, owner, "Research", ids.NewID())
			if err != nil {
				t.Fatal(err)
			}
			var until time.Time
			if role == domain.RoleGuest || role == domain.RoleClient {
				until = time.Now().Add(time.Hour)
			}
			member, err := domain.NewMember(ids.NewID(), firm.Org.ID, account, role, until, time.Now())
			if err != nil {
				t.Fatal(err)
			}
			if err := store.AddMember(ctx, member); err != nil {
				t.Fatal(err)
			}
			members := command.NewMembers(roleChangeStore{store: store}, pub, store, ids, clock.System{})
			if err := members.ChangeRole(ctx, account, firm.Org.ID, account, domain.RoleAdmin, time.Time{}); !errors.Is(err, orgquery.ErrNoAccess) {
				t.Fatalf("%s could change its own role: %v", role, err)
			}
			held, err := store.LiveMemberFor(ctx, firm.Org.ID, account)
			if err != nil || held.Role != role {
				t.Fatalf("role changed: %+v, %v", held, err)
			}
			if len(pub.events) != 1 {
				t.Fatalf("denied request published event: %+v", pub.events)
			}
		})
	}
}
