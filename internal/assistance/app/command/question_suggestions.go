package command

import (
	"context"

	assistapp "github.com/0xsj/overwatch-backend/internal/assistance/app"
	"github.com/0xsj/overwatch-backend/internal/assistance/domain"
	"github.com/0xsj/overwatch-backend/pkg/events"
	"github.com/0xsj/overwatch-backend/pkg/id"
	"github.com/0xsj/overwatch-backend/pkg/provenance"
)

type QuestionSuggestionRepository interface {
	CreateQuestionSuggestions(context.Context, domain.QuestionSuggestions) error
}

type QuestionSuggestionEvidence interface {
	LoadQuestionSuggestionInput(context.Context, id.ID, []domain.QuestionSuggestionGap) (assistapp.QuestionSuggestionInput, error)
}

type QuestionSuggestions struct {
	repo      QuestionSuggestionRepository
	evidence  QuestionSuggestionEvidence
	provider  assistapp.QuestionSuggestionProvider
	tx        Transactor
	publisher events.Publisher
	ids       Minter
	clock     Clock
}

func NewQuestionSuggestions(repo QuestionSuggestionRepository, evidence QuestionSuggestionEvidence, provider assistapp.QuestionSuggestionProvider, tx Transactor, publisher events.Publisher, ids Minter, clock Clock) *QuestionSuggestions {
	if repo == nil || evidence == nil || provider == nil || tx == nil || publisher == nil || ids == nil || clock == nil {
		panic("assistance: NewQuestionSuggestions with a nil dependency")
	}
	return &QuestionSuggestions{repo: repo, evidence: evidence, provider: provider, tx: tx, publisher: publisher, ids: ids, clock: clock}
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
	out, err := q.provider.SuggestQuestions(ctx, input)
	if err != nil {
		return domain.QuestionSuggestions{}, err
	}
	fresh, err := domain.NewQuestionSuggestions(q.ids.NewID(), workspace, actor, gaps, q.provider.Name(), q.provider.Method(), q.provider.TemplateVersion(), out.Status, out.Text, out.Suggestions, q.clock.Now())
	if err != nil {
		return domain.QuestionSuggestions{}, err
	}
	if err := q.tx.InTx(ctx, func(ctx context.Context) error {
		if err := q.repo.CreateQuestionSuggestions(ctx, fresh); err != nil {
			return err
		}
		prov, ok := provenance.Current(ctx)
		if !ok {
			prov = provenance.New(provenance.OriginRequest, q.ids)
		}
		prov, err := prov.WithTenant(workspace.String())
		if err != nil {
			return err
		}
		event, err := events.NewDecision(q.ids, q.clock, domain.EventQuestionSuggestionsGenerated, "workspace:"+workspace.String(), prov, map[string]any{
			"workspace_id": workspace.String(), "question_suggestions_id": fresh.ID.String(), "gap_count": len(fresh.Gaps), "suggestion_count": len(fresh.Suggestions), "provider": fresh.Provider, "method": fresh.Method, "template_version": fresh.TemplateVersion, "status": fresh.Status.String(), "actor": actor.String(),
		})
		if err != nil {
			return err
		}
		return q.publisher.Publish(ctx, event)
	}); err != nil {
		return domain.QuestionSuggestions{}, err
	}
	return fresh, nil
}
