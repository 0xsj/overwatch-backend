package command

import (
	"context"
	"strings"
	"unicode/utf8"

	assistapp "github.com/0xsj/overwatch-backend/internal/assistance/app"
	"github.com/0xsj/overwatch-backend/internal/assistance/domain"
	pkgerrors "github.com/0xsj/overwatch-backend/pkg/errors"
	"github.com/0xsj/overwatch-backend/pkg/events"
	"github.com/0xsj/overwatch-backend/pkg/id"
	"github.com/0xsj/overwatch-backend/pkg/provenance"
)

type QuestionSuggestionRepository interface {
	CreateQuestionSuggestions(context.Context, domain.QuestionSuggestions) error
	ProviderRunRepository
}

type QuestionSuggestionEvidence interface {
	LoadQuestionSuggestionInput(context.Context, id.ID, []domain.QuestionSuggestionGap) (assistapp.QuestionSuggestionInput, error)
}

type QuestionSuggestions struct {
	repo      QuestionSuggestionRepository
	evidence  QuestionSuggestionEvidence
	provider  assistapp.QuestionSuggestionProvider
	policy    ProviderPolicy
	tx        Transactor
	publisher events.Publisher
	ids       Minter
	clock     Clock
}

func NewQuestionSuggestions(repo QuestionSuggestionRepository, evidence QuestionSuggestionEvidence, provider assistapp.QuestionSuggestionProvider, tx Transactor, publisher events.Publisher, ids Minter, clock Clock) *QuestionSuggestions {
	if repo == nil || evidence == nil || provider == nil || tx == nil || publisher == nil || ids == nil || clock == nil {
		panic("assistance: NewQuestionSuggestions with a nil dependency")
	}
	return &QuestionSuggestions{repo: repo, evidence: evidence, provider: provider, policy: allowAllProviderPolicy{}, tx: tx, publisher: publisher, ids: ids, clock: clock}
}

func NewQuestionSuggestionsWithPolicy(repo QuestionSuggestionRepository, evidence QuestionSuggestionEvidence, provider assistapp.QuestionSuggestionProvider, policy ProviderPolicy, tx Transactor, publisher events.Publisher, ids Minter, clock Clock) *QuestionSuggestions {
	if repo == nil || evidence == nil || provider == nil || policy == nil || tx == nil || publisher == nil || ids == nil || clock == nil {
		panic("assistance: NewQuestionSuggestionsWithPolicy with a nil dependency")
	}
	return &QuestionSuggestions{repo: repo, evidence: evidence, provider: provider, policy: policy, tx: tx, publisher: publisher, ids: ids, clock: clock}
}

func (q *QuestionSuggestions) Generate(ctx context.Context, workspace id.ID, gaps []domain.QuestionSuggestionGap, actor id.ID) (domain.QuestionSuggestions, error) {
	if workspace.IsZero() || actor.IsZero() {
		return domain.QuestionSuggestions{}, domain.ErrIDRequired
	}
	if len(gaps) == 0 {
		return domain.QuestionSuggestions{}, domain.ErrQuestionSuggestionRequired
	}
	input, err := q.evidence.LoadQuestionSuggestionInput(ctx, workspace, gaps)
	if err != nil {
		return domain.QuestionSuggestions{}, err
	}
	timer := startProviderRun(input)
	if providerUsesExternal(q.provider) {
		allowed, err := q.policy.Current(ctx, workspace)
		if err != nil {
			return domain.QuestionSuggestions{}, err
		}
		if !allowed.AllowExternal {
			failed, buildErr := domain.NewQuestionSuggestionsResult(q.ids.NewID(), workspace, actor, gaps, q.provider.Name(), q.provider.Method(), q.provider.TemplateVersion(), domain.QuestionSuggestionsUnsupported, "", nil, domain.ErrExternalProviderDisabled.Error(), q.clock.Now())
			if buildErr != nil {
				return domain.QuestionSuggestions{}, buildErr
			}
			run, runErr := timer.finish(failed.ID, workspace, actor, "question_suggestions", failed.Provider, failed.Method, failed.TemplateVersion, failed.Status.String(), failed.Output, failed.Error, false, failed.CreatedAt)
			if runErr != nil {
				return domain.QuestionSuggestions{}, runErr
			}
			if persistErr := q.persist(ctx, failed, run, actor); persistErr != nil {
				return domain.QuestionSuggestions{}, persistErr
			}
			return failed, domain.ErrExternalProviderDisabled
		}
	}
	out, err := q.provider.SuggestQuestions(ctx, input)
	if err != nil {
		status := domain.QuestionSuggestionsFailed
		if pkgerrors.KindOf(err) == pkgerrors.Unavailable {
			status = domain.QuestionSuggestionsUnsupported
		} else if pkgerrors.KindOf(err) == pkgerrors.Timeout {
			status = domain.QuestionSuggestionsTimedOut
		}
		failed, buildErr := domain.NewQuestionSuggestionsResult(q.ids.NewID(), workspace, actor, gaps, q.provider.Name(), q.provider.Method(), q.provider.TemplateVersion(), status, "", nil, trimQuestionSuggestionError(err.Error()), q.clock.Now())
		if buildErr != nil {
			return domain.QuestionSuggestions{}, buildErr
		}
		run, runErr := timer.finish(failed.ID, workspace, actor, "question_suggestions", failed.Provider, failed.Method, failed.TemplateVersion, failed.Status.String(), failed.Output, failed.Error, status == domain.QuestionSuggestionsTimedOut, failed.CreatedAt)
		if runErr != nil {
			return domain.QuestionSuggestions{}, runErr
		}
		if persistErr := q.persist(ctx, failed, run, actor); persistErr != nil {
			return domain.QuestionSuggestions{}, persistErr
		}
		return failed, err
	}
	fresh, err := domain.NewQuestionSuggestions(q.ids.NewID(), workspace, actor, gaps, q.provider.Name(), q.provider.Method(), q.provider.TemplateVersion(), out.Status, out.Text, out.Suggestions, q.clock.Now())
	if err != nil {
		return domain.QuestionSuggestions{}, err
	}
	run, err := timer.finish(fresh.ID, workspace, actor, "question_suggestions", fresh.Provider, fresh.Method, fresh.TemplateVersion, fresh.Status.String(), fresh.Output, fresh.Error, false, fresh.CreatedAt)
	if err != nil {
		return domain.QuestionSuggestions{}, err
	}
	if err := q.persist(ctx, fresh, run, actor); err != nil {
		return domain.QuestionSuggestions{}, err
	}
	return fresh, nil
}

func (q *QuestionSuggestions) persist(ctx context.Context, fresh domain.QuestionSuggestions, run domain.ProviderRun, actor id.ID) error {
	return q.tx.InTx(ctx, func(ctx context.Context) error {
		if err := q.repo.CreateQuestionSuggestions(ctx, fresh); err != nil {
			return err
		}
		if err := q.repo.CreateProviderRun(ctx, run); err != nil {
			return err
		}
		prov, ok := provenance.Current(ctx)
		if !ok {
			prov = provenance.New(provenance.OriginRequest, q.ids)
		}
		prov, err := prov.WithTenant(fresh.WorkspaceID.String())
		if err != nil {
			return err
		}
		event, err := events.NewDecision(q.ids, q.clock, domain.EventQuestionSuggestionsGenerated, "workspace:"+fresh.WorkspaceID.String(), prov, map[string]any{
			"workspace_id": fresh.WorkspaceID.String(), "question_suggestions_id": fresh.ID.String(), "gap_count": len(fresh.Gaps), "suggestion_count": len(fresh.Suggestions), "provider": fresh.Provider, "method": fresh.Method, "template_version": fresh.TemplateVersion, "status": fresh.Status.String(), "error": fresh.Error, "actor": actor.String(),
		})
		if err != nil {
			return err
		}
		return q.publisher.Publish(ctx, event)
	})
}

func trimQuestionSuggestionError(raw string) string {
	failure := strings.TrimSpace(raw)
	if len(failure) <= domain.MaxQuestionSuggestionError {
		return failure
	}
	failure = failure[:domain.MaxQuestionSuggestionError]
	for !utf8.ValidString(failure) {
		failure = failure[:len(failure)-1]
	}
	return failure
}
