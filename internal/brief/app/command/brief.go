package command

import (
	"context"
	stderrors "errors"
	"time"

	"github.com/0xsj/overwatch-backend/internal/brief/domain"
	"github.com/0xsj/overwatch-backend/pkg/events"
	"github.com/0xsj/overwatch-backend/pkg/id"
	"github.com/0xsj/overwatch-backend/pkg/provenance"
)

type Repository interface {
	Create(context.Context, domain.Brief) error
	ByWorkspace(context.Context, id.ID) (domain.Brief, error)
	Save(context.Context, domain.Brief) error
	ReplaceObservations(context.Context, id.ID, id.ID, []id.ID) error
	ReplaceQuestions(context.Context, id.ID, id.ID, []id.ID) error
	ReplaceConnections(context.Context, id.ID, id.ID, []id.ID) error
	QuestionSnapshots(context.Context, id.ID, []id.ID) ([]domain.QuestionSnapshot, error)
	ConnectionSnapshots(context.Context, id.ID, []id.ID) ([]domain.ConnectionSnapshot, error)
	CreateSnapshot(context.Context, domain.Snapshot) error
	ReplaceSnapshotObservations(context.Context, id.ID, id.ID, []id.ID) error
	ReplaceSnapshotQuestions(context.Context, id.ID, id.ID, []domain.QuestionSnapshot) error
	ReplaceSnapshotConnections(context.Context, id.ID, id.ID, []domain.ConnectionSnapshot) error
}
type Transactor interface {
	InTx(context.Context, func(context.Context) error) error
}
type Minter interface{ NewID() id.ID }
type Clock interface{ Now() time.Time }

type Briefs struct {
	repo      Repository
	tx        Transactor
	publisher events.Publisher
	ids       Minter
	clock     Clock
}

func NewBriefs(repo Repository, tx Transactor, publisher events.Publisher, ids Minter, clock Clock) *Briefs {
	if repo == nil || tx == nil || publisher == nil || ids == nil || clock == nil {
		panic("brief: NewBriefs with a nil dependency")
	}
	return &Briefs{repo: repo, tx: tx, publisher: publisher, ids: ids, clock: clock}
}

// Save creates the workspace's singleton brief on first write and edits it
// thereafter. The read-before-write is intentional: the brief has one identity
// per investigation, so callers never choose an id that could fork it.
func (b *Briefs) Save(ctx context.Context, workspace, author id.ID, title, question, currentAccount, alternatives, limitations, nextSteps string, observations, questions, connections []id.ID) (domain.Brief, error) {
	held, err := b.repo.ByWorkspace(ctx, workspace)
	edit := err == nil
	if err != nil && !stderrors.Is(err, domain.ErrNotFound) {
		return domain.Brief{}, err
	}
	var next domain.Brief
	if edit {
		next, err = held.Edit(author, title, question, currentAccount, alternatives, limitations, nextSteps, observations, questions, connections, b.clock.Now())
	} else {
		next, err = domain.New(b.ids.NewID(), workspace, author, title, question, currentAccount, alternatives, limitations, nextSteps, observations, questions, connections, b.clock.Now())
	}
	if err != nil {
		return domain.Brief{}, err
	}
	if err := b.tx.InTx(ctx, func(ctx context.Context) error {
		if edit {
			if err := b.repo.Save(ctx, next); err != nil {
				return err
			}
		} else if err := b.repo.Create(ctx, next); err != nil {
			return err
		}
		if err := b.repo.ReplaceObservations(ctx, workspace, next.ID, next.ObservationIDs); err != nil {
			return err
		}
		if err := b.repo.ReplaceQuestions(ctx, workspace, next.ID, next.QuestionIDs); err != nil {
			return err
		}
		if err := b.repo.ReplaceConnections(ctx, workspace, next.ID, next.ConnectionIDs); err != nil {
			return err
		}
		return b.publish(ctx, workspace, next, edit)
	}); err != nil {
		return domain.Brief{}, err
	}
	return next, nil
}

func (b *Briefs) publish(ctx context.Context, workspace id.ID, brief domain.Brief, edit bool) error {
	prov, ok := provenance.Current(ctx)
	if !ok {
		prov = provenance.New(provenance.OriginRequest, b.ids)
	}
	prov, err := prov.WithTenant(workspace.String())
	if err != nil {
		return err
	}
	event, err := events.NewDecision(b.ids, b.clock, domain.EventChanged, "workspace:"+workspace.String(), prov, domain.Changed{WorkspaceID: workspace.String(), BriefID: brief.ID.String(), UpdatedBy: brief.UpdatedBy.String(), Edit: edit})
	if err != nil {
		return err
	}
	return b.publisher.Publish(ctx, event)
}

// Freeze copies the current authored brief into an append-only handoff. The
// source brief remains editable; this operation gives downstream consumers a
// stable point-in-time record rather than pretending the working row is one.
func (b *Briefs) Freeze(ctx context.Context, workspace, frozenBy id.ID) (domain.Snapshot, error) {
	var snapshot domain.Snapshot
	if err := b.tx.InTx(ctx, func(ctx context.Context) error {
		brief, err := b.repo.ByWorkspace(ctx, workspace)
		if err != nil {
			return err
		}
		questions, err := b.repo.QuestionSnapshots(ctx, workspace, brief.QuestionIDs)
		if err != nil {
			return err
		}
		connections, err := b.repo.ConnectionSnapshots(ctx, workspace, brief.ConnectionIDs)
		if err != nil {
			return err
		}
		snapshot, err = domain.NewSnapshot(b.ids.NewID(), frozenBy, brief, questions, connections, b.clock.Now())
		if err != nil {
			return err
		}
		if err := b.repo.CreateSnapshot(ctx, snapshot); err != nil {
			return err
		}
		if err := b.repo.ReplaceSnapshotObservations(ctx, workspace, snapshot.ID, snapshot.ObservationIDs); err != nil {
			return err
		}
		if err := b.repo.ReplaceSnapshotQuestions(ctx, workspace, snapshot.ID, snapshot.Questions); err != nil {
			return err
		}
		if err := b.repo.ReplaceSnapshotConnections(ctx, workspace, snapshot.ID, snapshot.Connections); err != nil {
			return err
		}
		return b.publishSnapshot(ctx, workspace, snapshot)
	}); err != nil {
		return domain.Snapshot{}, err
	}
	return snapshot, nil
}

func (b *Briefs) publishSnapshot(ctx context.Context, workspace id.ID, snapshot domain.Snapshot) error {
	prov, ok := provenance.Current(ctx)
	if !ok {
		prov = provenance.New(provenance.OriginRequest, b.ids)
	}
	prov, err := prov.WithTenant(workspace.String())
	if err != nil {
		return err
	}
	event, err := events.NewDecision(b.ids, b.clock, domain.EventSnapshotCreated, "workspace:"+workspace.String(), prov, domain.SnapshotCreated{WorkspaceID: workspace.String(), SnapshotID: snapshot.ID.String(), BriefID: snapshot.BriefID.String(), FrozenBy: snapshot.FrozenBy.String()})
	if err != nil {
		return err
	}
	return b.publisher.Publish(ctx, event)
}
