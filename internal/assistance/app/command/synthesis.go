package command

import (
	"context"

	assistapp "github.com/0xsj/overwatch-backend/internal/assistance/app"
	"github.com/0xsj/overwatch-backend/internal/assistance/domain"
	"github.com/0xsj/overwatch-backend/pkg/events"
	"github.com/0xsj/overwatch-backend/pkg/id"
	"github.com/0xsj/overwatch-backend/pkg/provenance"
)

type SynthesisRepository interface {
	CreateSynthesis(context.Context, domain.Synthesis) error
}

type EvidenceReader interface {
	Evidence(context.Context, id.ID, id.ID) (assistapp.Observation, error)
}

type Syntheses struct {
	repo      SynthesisRepository
	evidence  EvidenceReader
	provider  assistapp.SynthesisProvider
	tx        Transactor
	publisher events.Publisher
	ids       Minter
	clock     Clock
}

func NewSyntheses(repo SynthesisRepository, evidence EvidenceReader, provider assistapp.SynthesisProvider, tx Transactor, publisher events.Publisher, ids Minter, clock Clock) *Syntheses {
	if repo == nil || evidence == nil || provider == nil || tx == nil || publisher == nil || ids == nil || clock == nil {
		panic("assistance: NewSyntheses with a nil dependency")
	}
	return &Syntheses{repo: repo, evidence: evidence, provider: provider, tx: tx, publisher: publisher, ids: ids, clock: clock}
}

func (s *Syntheses) Generate(ctx context.Context, workspace id.ID, observations []id.ID, actor id.ID) (domain.Synthesis, error) {
	if workspace.IsZero() || actor.IsZero() {
		return domain.Synthesis{}, domain.ErrIDRequired
	}
	if len(observations) == 0 {
		return domain.Synthesis{}, domain.ErrSynthesisRequired
	}
	if len(observations) > domain.MaxSynthesisObservations {
		return domain.Synthesis{}, domain.ErrSynthesisTooLarge
	}
	inputs := make([]assistapp.Observation, 0, len(observations))
	seen := make(map[id.ID]struct{}, len(observations))
	for _, observationID := range observations {
		if _, exists := seen[observationID]; exists {
			return domain.Synthesis{}, domain.ErrCandidateInvalid
		}
		found, err := s.evidence.Evidence(ctx, workspace, observationID)
		if err != nil {
			return domain.Synthesis{}, err
		}
		if found.ID != observationID || found.WorkspaceID != workspace {
			return domain.Synthesis{}, domain.ErrNotFound
		}
		seen[observationID] = struct{}{}
		inputs = append(inputs, found)
	}
	out, err := s.provider.Synthesize(ctx, inputs)
	if err != nil {
		return domain.Synthesis{}, err
	}
	fresh, err := domain.NewSynthesis(s.ids.NewID(), workspace, actor, observations, s.provider.Name(), s.provider.Method(), out.Text, out.Candidates, s.clock.Now())
	if err != nil {
		return domain.Synthesis{}, err
	}
	if err := s.tx.InTx(ctx, func(ctx context.Context) error {
		if err := s.repo.CreateSynthesis(ctx, fresh); err != nil {
			return err
		}
		prov, ok := provenance.Current(ctx)
		if !ok {
			prov = provenance.New(provenance.OriginRequest, s.ids)
		}
		prov, err := prov.WithTenant(workspace.String())
		if err != nil {
			return err
		}
		event, err := events.NewDecision(s.ids, s.clock, domain.EventSynthesisGenerated, "workspace:"+workspace.String(), prov, map[string]any{
			"workspace_id": workspace.String(), "synthesis_id": fresh.ID.String(), "observation_count": len(fresh.ObservationIDs), "candidate_count": len(fresh.Candidates), "provider": fresh.Provider, "method": fresh.Method, "actor": actor.String(),
		})
		if err != nil {
			return err
		}
		return s.publisher.Publish(ctx, event)
	}); err != nil {
		return domain.Synthesis{}, err
	}
	return fresh, nil
}
