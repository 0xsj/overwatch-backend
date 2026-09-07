package command

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/0xsj/overwatch-backend/internal/org/domain"
	"github.com/0xsj/overwatch-backend/pkg/events"
	"github.com/0xsj/overwatch-backend/pkg/id"
	"github.com/0xsj/overwatch-backend/pkg/provenance"
)

// AccountArchived is the event this listens for. A string rather than an import
// of identity's constant: org and identity are peers, and the coupling is a
// contract on a name — which is what it would be over a broker too.
const AccountArchived = "identity.account.archived"

type accountArchived struct {
	AccountID string `json:"account_id"`
}

// Departures ends every membership an archived account held — decisions/0028.
//
// **The last-owner check happened before the account was archived**, at the
// composition root, because identity may not read org's tables. If this finds a
// membership it cannot end — the race that record names — it FAILS rather than
// skipping: the event is retried and eventually buried, and a buried outbox row
// is the alarm. A silent skip would leave an org owned by somebody who no longer
// exists, with nothing anywhere saying so.
type Departures struct {
	repo  MemberRepository
	roles DepartureReader
	ids   Minter
	clock Clock
}

// DepartureReader is the one read this needs beyond MemberRepository: which orgs
// the account was in at all.
type DepartureReader interface {
	OrgsForAccount(ctx context.Context, account id.ID) ([]domain.Org, error)
}

func NewDepartures(repo MemberRepository, roles DepartureReader, ids Minter, clock Clock) *Departures {
	if repo == nil || roles == nil || ids == nil || clock == nil {
		panic("org: NewDepartures with a nil dependency")
	}
	return &Departures{repo: repo, roles: roles, ids: ids, clock: clock}
}

func (d *Departures) Handle(ctx context.Context, e events.Event) error {
	if e.Name != AccountArchived {
		return nil
	}
	var payload accountArchived
	if len(e.Payload) > 0 {
		if err := json.Unmarshal(e.Payload, &payload); err != nil {
			return fmt.Errorf("org: decode %s: %w", e.Name, err)
		}
	}
	account, err := id.Parse(payload.AccountID)
	if err != nil {
		// The subject names the account too, and is the contract 0013 put
		// there — but this payload is org's only source for the id when the
		// subject is somebody else's vocabulary.
		if account, err = id.Parse(e.SubjectID()); err != nil {
			return fmt.Errorf("org: %s names no account: %w", e.Name, err)
		}
	}
	if e.Provenance.IsZero() {
		return fmt.Errorf("org: %s carries no provenance", e.Name)
	}
	prov, err := e.Provenance.DeriveFrom(d.ids, e.ID)
	if err != nil {
		return err
	}
	ctx = provenance.NewContext(ctx, prov)

	orgs, err := d.roles.OrgsForAccount(ctx, account)
	if err != nil {
		return fmt.Errorf("org: departures: %w", err)
	}
	at := d.clock.Now()
	for _, org := range orgs {
		member, err := d.repo.LiveMemberFor(ctx, org.ID, account)
		if err != nil {
			// Already gone. A redelivery is not a failure — decisions/0007.
			continue
		}
		if member.Role == domain.RoleOwner {
			owners, err := d.repo.LockLiveOwners(ctx, org.ID)
			if err != nil {
				return fmt.Errorf("org: departures: %w", err)
			}
			remaining := 0
			for _, owner := range owners {
				if owner != account {
					remaining++
				}
			}
			members, err := d.repo.MembersOf(ctx, org.ID)
			if err != nil {
				return fmt.Errorf("org: departures: %w", err)
			}
			others := 0
			for _, m := range members {
				if m.AccountID != account {
					others++
				}
			}
			// An org that is only them strands nobody and may be left
			// memberless. One with other people and no other owner is the race
			// decisions/0028 names — fail loudly rather than force it.
			if remaining == 0 && others > 0 {
				return fmt.Errorf("org: %s would lose its last owner to a closed account: %w",
					org.ID, domain.ErrLastOwner)
			}
		}
		gone, err := member.Archive(at)
		if err != nil {
			return fmt.Errorf("org: departures: %w", err)
		}
		if err := d.repo.SaveMember(ctx, gone); err != nil {
			return fmt.Errorf("org: departures: %w", err)
		}
		if _, err := d.repo.RevokeGrantsFor(ctx, org.ID, account); err != nil {
			return fmt.Errorf("org: departures: %w", err)
		}
	}
	return nil
}
