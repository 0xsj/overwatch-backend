package command

import (
	"context"
	stderrors "errors"
	"strings"
	"unicode/utf8"

	assistapp "github.com/0xsj/overwatch-backend/internal/assistance/app"
	"github.com/0xsj/overwatch-backend/internal/assistance/domain"
	pkgerrors "github.com/0xsj/overwatch-backend/pkg/errors"
	"github.com/0xsj/overwatch-backend/pkg/events"
	"github.com/0xsj/overwatch-backend/pkg/id"
	"github.com/0xsj/overwatch-backend/pkg/provenance"
)

type SynthesisRepository interface {
	CreateSynthesis(context.Context, domain.Synthesis) error
	ProviderRunRepository
}

type EvidenceReader interface {
	Evidence(context.Context, id.ID, id.ID) (assistapp.Observation, error)
}

type Syntheses struct {
	repo      SynthesisRepository
	evidence  EvidenceReader
	provider  assistapp.SynthesisProvider
	policy    ProviderPolicy
	tx        Transactor
	publisher events.Publisher
	ids       Minter
	clock     Clock
}

func NewSyntheses(repo SynthesisRepository, evidence EvidenceReader, provider assistapp.SynthesisProvider, tx Transactor, publisher events.Publisher, ids Minter, clock Clock) *Syntheses {
	if repo == nil || evidence == nil || provider == nil || tx == nil || publisher == nil || ids == nil || clock == nil {
		panic("assistance: NewSyntheses with a nil dependency")
	}
	return &Syntheses{repo: repo, evidence: evidence, provider: provider, policy: allowAllProviderPolicy{}, tx: tx, publisher: publisher, ids: ids, clock: clock}
}

func NewSynthesesWithPolicy(repo SynthesisRepository, evidence EvidenceReader, provider assistapp.SynthesisProvider, policy ProviderPolicy, tx Transactor, publisher events.Publisher, ids Minter, clock Clock) *Syntheses {
	if repo == nil || evidence == nil || provider == nil || policy == nil || tx == nil || publisher == nil || ids == nil || clock == nil {
		panic("assistance: NewSynthesesWithPolicy with a nil dependency")
	}
	return &Syntheses{repo: repo, evidence: evidence, provider: provider, policy: policy, tx: tx, publisher: publisher, ids: ids, clock: clock}
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
	timer := startProviderRun(inputs)
	if synthesisProviderUsesExternal(s.provider) {
		allowed, err := s.policy.Current(ctx, workspace)
		if err != nil {
			return domain.Synthesis{}, err
		}
		if !allowed.AllowExternal {
			failed, buildErr := domain.NewSynthesisResult(s.ids.NewID(), workspace, actor, observations, s.provider.Name(), s.provider.Method(), domain.SynthesisUnsupported, "", nil, domain.ErrExternalProviderDisabled.Error(), s.clock.Now())
			if buildErr != nil {
				return domain.Synthesis{}, buildErr
			}
			run, runErr := timer.finish(failed.ID, workspace, actor, "synthesis", failed.Provider, failed.Method, failed.Method, failed.Status.String(), failed.Output, failed.Error, false, failed.CreatedAt)
			if runErr != nil {
				return domain.Synthesis{}, runErr
			}
			if persistErr := s.persist(ctx, failed, run, actor); persistErr != nil {
				return domain.Synthesis{}, persistErr
			}
			return failed, domain.ErrExternalProviderDisabled
		}
	}
	out, err := s.provider.Synthesize(ctx, inputs)
	if err != nil {
		status := domain.SynthesisFailed
		if stderrors.Is(err, assistapp.ErrSynthesisProviderUnavailable) {
			status = domain.SynthesisUnsupported
		} else if pkgerrors.KindOf(err) == pkgerrors.Timeout {
			status = domain.SynthesisTimedOut
		}
		failed, buildErr := domain.NewSynthesisResult(s.ids.NewID(), workspace, actor, observations, s.provider.Name(), s.provider.Method(), status, "", nil, trimSynthesisError(err.Error()), s.clock.Now())
		if buildErr != nil {
			return domain.Synthesis{}, buildErr
		}
		run, runErr := timer.finish(failed.ID, workspace, actor, "synthesis", failed.Provider, failed.Method, failed.Method, failed.Status.String(), failed.Output, failed.Error, status == domain.SynthesisTimedOut, failed.CreatedAt)
		if runErr != nil {
			return domain.Synthesis{}, runErr
		}
		if persistErr := s.persist(ctx, failed, run, actor); persistErr != nil {
			return domain.Synthesis{}, persistErr
		}
		return failed, err
	}
	fresh, err := domain.NewSynthesis(s.ids.NewID(), workspace, actor, observations, s.provider.Name(), s.provider.Method(), out.Text, out.Candidates, s.clock.Now())
	if err != nil {
		return domain.Synthesis{}, err
	}
	run, err := timer.finish(fresh.ID, workspace, actor, "synthesis", fresh.Provider, fresh.Method, fresh.Method, fresh.Status.String(), fresh.Output, fresh.Error, false, fresh.CreatedAt)
	if err != nil {
		return domain.Synthesis{}, err
	}
	if err := s.persist(ctx, fresh, run, actor); err != nil {
		return domain.Synthesis{}, err
	}
	return fresh, nil
}

func synthesisProviderUsesExternal(provider assistapp.SynthesisProvider) bool {
	external, ok := provider.(interface{ External() bool })
	return !ok || external.External()
}

type allowAllProviderPolicy struct{}

func (allowAllProviderPolicy) Current(_ context.Context, workspace id.ID) (domain.ProviderPolicy, error) {
	return domain.ProviderPolicy{WorkspaceID: workspace, AllowExternal: true}, nil
}

func (s *Syntheses) persist(ctx context.Context, fresh domain.Synthesis, run domain.ProviderRun, actor id.ID) error {
	return s.tx.InTx(ctx, func(ctx context.Context) error {
		if err := s.repo.CreateSynthesis(ctx, fresh); err != nil {
			return err
		}
		if err := s.repo.CreateProviderRun(ctx, run); err != nil {
			return err
		}
		prov, ok := provenance.Current(ctx)
		if !ok {
			prov = provenance.New(provenance.OriginRequest, s.ids)
		}
		prov, err := prov.WithTenant(fresh.WorkspaceID.String())
		if err != nil {
			return err
		}
		event, err := events.NewDecision(s.ids, s.clock, domain.EventSynthesisGenerated, "workspace:"+fresh.WorkspaceID.String(), prov, map[string]any{
			"workspace_id": fresh.WorkspaceID.String(), "synthesis_id": fresh.ID.String(), "observation_count": len(fresh.ObservationIDs), "candidate_count": len(fresh.Candidates), "provider": fresh.Provider, "method": fresh.Method, "status": fresh.Status.String(), "error": fresh.Error, "actor": actor.String(),
		})
		if err != nil {
			return err
		}
		return s.publisher.Publish(ctx, event)
	})
}

func trimSynthesisError(raw string) string {
	failure := strings.TrimSpace(raw)
	if len(failure) <= domain.MaxSynthesisError {
		return failure
	}
	failure = failure[:domain.MaxSynthesisError]
	for !utf8.ValidString(failure) {
		failure = failure[:len(failure)-1]
	}
	return failure
}
