package command

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/0xsj/overwatch-backend/internal/workspace/domain"
	"github.com/0xsj/overwatch-backend/pkg/errors"
	"github.com/0xsj/overwatch-backend/pkg/events"
	"github.com/0xsj/overwatch-backend/pkg/id"
	"github.com/0xsj/overwatch-backend/pkg/provenance"
)

const OrgCreated = "org.created"

// orgCreated is the one field this package reads out of org's payload, declared
// here rather than imported. Importing org's type would be a peer import the
// checks refuse, and copying one field is the cost of that — see doc.go.
type orgCreated struct {
	OwnerID string `json:"owner_id"`
}

type Subscriber struct {
	service *Service
	ids     Minter
}

func NewSubscriber(service *Service, ids Minter) *Subscriber {
	if service == nil || ids == nil {
		panic("workspace: NewSubscriber with a nil dependency")
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
		return provenance.Provenance{}, fmt.Errorf("workspace: %s carries no provenance", e.Name)
	}
	return e.Provenance.DeriveFrom(s.ids, e.ID)
}

// Handle reads the subject for the org id and the payload for ONE field.
//
// It decoded nothing until decisions/0020, and the change is worth naming: the
// org id is still the envelope's subject, but `created_by` cannot be computed
// from anything this package can see. The registration request is
// unauthenticated, so the actor is `anonymous` and the provenance cannot supply
// it either — which is exactly the case decisions/0013 describes: what a
// subscriber cannot compute belongs in the message.
func (s *Subscriber) Handle(ctx context.Context, e events.Event) error {
	if e.Name != OrgCreated {
		return nil
	}
	org, err := id.Parse(e.SubjectID())
	if err != nil {
		return fmt.Errorf("workspace: %s has no readable subject %q: %w", e.Name, e.Subject, err)
	}
	var payload orgCreated
	if err := json.Unmarshal(e.Payload, &payload); err != nil {
		return fmt.Errorf("workspace: decode %s: %w", e.Name, err)
	}
	owner, err := id.Parse(payload.OwnerID)
	if err != nil {
		return fmt.Errorf("workspace: %s names no owner: %w", e.Name, err)
	}
	prov, err := s.caused(e)
	if err != nil {
		return err
	}
	if _, err := s.service.Provision(provenance.NewContext(ctx, prov), org, owner, "", e.ID); err != nil {
		if errors.Is(err, domain.ErrAlreadyProvisioned) {
			return nil
		}
		return err
	}
	return nil
}
