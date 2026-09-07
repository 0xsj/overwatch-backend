package command

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/0xsj/overwatch-backend/internal/org/domain"
	"github.com/0xsj/overwatch-backend/pkg/errors"
	"github.com/0xsj/overwatch-backend/pkg/events"
	"github.com/0xsj/overwatch-backend/pkg/id"
	"github.com/0xsj/overwatch-backend/pkg/provenance"
)

const AccountCreated = "identity.account.created"

type accountCreated struct {
	Name string `json:"name"`
}

type Subscriber struct {
	service *Service
	ids     Minter
}

func NewSubscriber(service *Service, ids Minter) *Subscriber {
	if service == nil || ids == nil {
		panic("org: NewSubscriber with a nil dependency")
	}
	return &Subscriber{service: service, ids: ids}
}

// caused derives this link's provenance from the event that asked for it, so
// the whole chain shares ONE correlation id and each step's causation names the
// event before it. Without it every subscriber starts a fresh chain, and the
// journal — whose subtitle is "what caused this" — cannot join the rows a single
// registration produces. Observed on a live run before it was fixed.
func (s *Subscriber) caused(e events.Event) (provenance.Provenance, error) {
	if e.Provenance.IsZero() {
		return provenance.Provenance{}, fmt.Errorf("org: %s carries no provenance", e.Name)
	}
	return e.Provenance.DeriveFrom(s.ids, e.ID)
}

func (s *Subscriber) Handle(ctx context.Context, e events.Event) error {
	if e.Name != AccountCreated {
		return nil
	}
	owner, err := id.Parse(e.SubjectID())
	if err != nil {
		return fmt.Errorf("org: %s has no readable subject %q: %w", e.Name, e.Subject, err)
	}
	var payload accountCreated
	if len(e.Payload) > 0 {
		if err := json.Unmarshal(e.Payload, &payload); err != nil {
			return fmt.Errorf("org: decode %s: %w", e.Name, err)
		}
	}
	prov, err := s.caused(e)
	if err != nil {
		return err
	}
	if _, err := s.service.Provision(provenance.NewContext(ctx, prov), owner, payload.Name, e.ID); err != nil {
		if errors.Is(err, domain.ErrAlreadyProvisioned) {
			return nil
		}
		return err
	}
	return nil
}
