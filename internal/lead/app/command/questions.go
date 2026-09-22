package command

import (
	"context"
	"time"

	"github.com/0xsj/overwatch-backend/internal/lead/domain"
	"github.com/0xsj/overwatch-backend/pkg/events"
	"github.com/0xsj/overwatch-backend/pkg/id"
	"github.com/0xsj/overwatch-backend/pkg/provenance"
)

type Repository interface {
	Create(context.Context, domain.Question) error
	ByID(context.Context, id.ID, id.ID) (domain.Question, error)
	Save(context.Context, domain.Question) error
	ReplaceObservations(context.Context, id.ID, id.ID, []id.ID) error
}
type Transactor interface {
	InTx(context.Context, func(context.Context) error) error
}
type Minter interface{ NewID() id.ID }
type Clock interface{ Now() time.Time }

type Questions struct {
	repo      Repository
	tx        Transactor
	publisher events.Publisher
	ids       Minter
	clock     Clock
}

func NewQuestions(repo Repository, tx Transactor, publisher events.Publisher, ids Minter, clock Clock) *Questions {
	if repo == nil || tx == nil || publisher == nil || ids == nil || clock == nil {
		panic("lead: NewQuestions with a nil dependency")
	}
	return &Questions{repo: repo, tx: tx, publisher: publisher, ids: ids, clock: clock}
}

func (q *Questions) Create(ctx context.Context, workspace, author id.ID,
	prompt, contextText, state, resolution string, observations []id.ID) (domain.Question, error) {
	return q.CreateWithContext(ctx, workspace, author, "", id.ID{}, prompt, contextText, state, resolution, observations)
}

func (q *Questions) CreateWithContext(ctx context.Context, workspace, author id.ID, contextKind string, contextID id.ID,
	prompt, contextText, state, resolution string, observations []id.ID) (domain.Question, error) {
	fresh, err := domain.NewWithContext(q.ids.NewID(), workspace, author, contextKind, contextID, prompt, contextText, state, resolution, observations, q.clock.Now())
	if err != nil {
		return domain.Question{}, err
	}
	if err := q.tx.InTx(ctx, func(ctx context.Context) error {
		if err := q.repo.Create(ctx, fresh); err != nil {
			return err
		}
		if err := q.repo.ReplaceObservations(ctx, workspace, fresh.ID, fresh.ObservationIDs); err != nil {
			return err
		}
		return q.publish(ctx, workspace, fresh, false)
	}); err != nil {
		return domain.Question{}, err
	}
	return fresh, nil
}

func (q *Questions) Edit(ctx context.Context, workspace, want, editor id.ID,
	prompt, contextText, state, resolution string, observations []id.ID) (domain.Question, error) {
	return q.EditWithContext(ctx, workspace, want, editor, "", id.ID{}, prompt, contextText, state, resolution, observations)
}

func (q *Questions) EditWithContext(ctx context.Context, workspace, want, editor id.ID, contextKind string, contextID id.ID,
	prompt, contextText, state, resolution string, observations []id.ID) (domain.Question, error) {
	held, err := q.repo.ByID(ctx, workspace, want)
	if err != nil {
		return domain.Question{}, err
	}
	next, err := held.EditWithContext(editor, contextKind, contextID, prompt, contextText, state, resolution, observations, q.clock.Now())
	if err != nil {
		return domain.Question{}, err
	}
	if err := q.tx.InTx(ctx, func(ctx context.Context) error {
		if err := q.repo.Save(ctx, next); err != nil {
			return err
		}
		if err := q.repo.ReplaceObservations(ctx, workspace, next.ID, next.ObservationIDs); err != nil {
			return err
		}
		return q.publish(ctx, workspace, next, true)
	}); err != nil {
		return domain.Question{}, err
	}
	return next, nil
}

func (q *Questions) publish(ctx context.Context, workspace id.ID, question domain.Question, edit bool) error {
	prov, ok := provenance.Current(ctx)
	if !ok {
		prov = provenance.New(provenance.OriginRequest, q.ids)
	}
	tenanted, err := prov.WithTenant(workspace.String())
	if err != nil {
		return err
	}
	event, err := events.NewDecision(q.ids, q.clock, domain.EventChanged,
		"workspace:"+workspace.String(), tenanted, domain.Changed{
			WorkspaceID: workspace.String(), QuestionID: question.ID.String(),
			State: question.State.String(), UpdatedBy: question.UpdatedBy.String(), Edit: edit,
		})
	if err != nil {
		return err
	}
	return q.publisher.Publish(ctx, event)
}
