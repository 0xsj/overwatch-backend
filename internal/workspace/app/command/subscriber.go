package command

import (
	"context"
	"fmt"

	"github.com/0xsj/overwatch-backend/internal/workspace/domain"
	"github.com/0xsj/overwatch-backend/pkg/errors"
	"github.com/0xsj/overwatch-backend/pkg/events"
	"github.com/0xsj/overwatch-backend/pkg/id"
	"github.com/0xsj/overwatch-backend/pkg/provenance"
)

const OrgCreated = "org.created"

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

// Handle decodes NOTHING. The org id is the event's subject and the name is this
// package's own default, so the only contract it depends on is the envelope —
// which is what decisions/0013 put the subject there for.
func (s *Subscriber) Handle(ctx context.Context, e events.Event) error {
	if e.Name != OrgCreated {
		return nil
	}
	org, err := id.Parse(e.SubjectID())
	if err != nil {
		return fmt.Errorf("workspace: %s has no readable subject %q: %w", e.Name, e.Subject, err)
	}
	prov, err := s.caused(e)
	if err != nil {
		return err
	}
	if _, err := s.service.Provision(provenance.NewContext(ctx, prov), org, "", e.ID); err != nil {
		if errors.Is(err, domain.ErrAlreadyProvisioned) {
			return nil
		}
		return err
	}
	return nil
}
