package command

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
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
	ReplaceClusters(context.Context, id.ID, id.ID, []id.ID) error
	ReplaceQuestions(context.Context, id.ID, id.ID, []id.ID) error
	ReplaceConnections(context.Context, id.ID, id.ID, []id.ID) error
	ReplaceEvents(context.Context, id.ID, id.ID, []id.ID) error
	QuestionSnapshots(context.Context, id.ID, []id.ID) ([]domain.QuestionSnapshot, error)
	ClusterSnapshots(context.Context, id.ID, []id.ID) ([]domain.ClusterSnapshot, error)
	ConnectionSnapshots(context.Context, id.ID, []id.ID) ([]domain.ConnectionSnapshot, error)
	EventSnapshots(context.Context, id.ID, []id.ID) ([]domain.EventSnapshot, error)
	EventRelationshipSnapshots(context.Context, id.ID, []id.ID) ([]domain.EventRelationshipSnapshot, error)
	CreateSnapshot(context.Context, domain.Snapshot) error
	ReplaceSnapshotObservations(context.Context, id.ID, id.ID, []id.ID) error
	ReplaceSnapshotClusters(context.Context, id.ID, id.ID, []domain.ClusterSnapshot) error
	ReplaceSnapshotQuestions(context.Context, id.ID, id.ID, []domain.QuestionSnapshot) error
	ReplaceSnapshotConnections(context.Context, id.ID, id.ID, []domain.ConnectionSnapshot) error
	ReplaceSnapshotEvents(context.Context, id.ID, id.ID, []domain.EventSnapshot) error
	ReplaceSnapshotEventRelationships(context.Context, id.ID, id.ID, []domain.EventRelationshipSnapshot) error
	SnapshotReview(context.Context, id.ID, id.ID) (domain.SnapshotReview, error)
	EnsureSnapshotReview(context.Context, id.ID, id.ID, time.Time) error
	AssignSnapshotReviewer(context.Context, id.ID, id.ID, *id.ID, id.ID, time.Time) error
	AddReviewDecision(context.Context, domain.ReviewDecision) error
	CreateSnapshotComment(context.Context, domain.SnapshotComment) error
	CreateSnapshotShare(context.Context, domain.HandoffShare) error
	RevokeSnapshotShare(context.Context, id.ID, id.ID, id.ID, time.Time) (domain.HandoffShare, error)
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
	if err != nil && !stderrors.Is(err, domain.ErrNotFound) {
		return domain.Brief{}, err
	}
	var events []id.ID
	var clusters []id.ID
	if err == nil {
		events = held.EventIDs
		clusters = held.ClusterIDs
	}
	return b.SaveWithEventsAndClusters(ctx, workspace, author, title, question, currentAccount, alternatives, limitations, nextSteps, observations, clusters, questions, connections, events)
}

func (b *Briefs) SaveWithEvents(ctx context.Context, workspace, author id.ID, title, question, currentAccount, alternatives, limitations, nextSteps string, observations, questions, connections, eventIDs []id.ID) (domain.Brief, error) {
	held, err := b.repo.ByWorkspace(ctx, workspace)
	if err != nil && !stderrors.Is(err, domain.ErrNotFound) {
		return domain.Brief{}, err
	}
	var clusters []id.ID
	if err == nil {
		clusters = held.ClusterIDs
	}
	return b.SaveWithEventsAndClusters(ctx, workspace, author, title, question, currentAccount, alternatives, limitations, nextSteps, observations, clusters, questions, connections, eventIDs)
}

func (b *Briefs) SaveWithEventsAndClusters(ctx context.Context, workspace, author id.ID, title, question, currentAccount, alternatives, limitations, nextSteps string, observations, clusters, questions, connections, eventIDs []id.ID) (domain.Brief, error) {
	held, err := b.repo.ByWorkspace(ctx, workspace)
	edit := err == nil
	if err != nil && !stderrors.Is(err, domain.ErrNotFound) {
		return domain.Brief{}, err
	}
	var next domain.Brief
	if edit {
		next, err = held.EditWithEventsAndClusters(author, title, question, currentAccount, alternatives, limitations, nextSteps, observations, clusters, questions, connections, eventIDs, b.clock.Now())
	} else {
		next, err = domain.NewWithEventsAndClusters(b.ids.NewID(), workspace, author, title, question, currentAccount, alternatives, limitations, nextSteps, observations, clusters, questions, connections, eventIDs, b.clock.Now())
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
		if err := b.repo.ReplaceClusters(ctx, workspace, next.ID, next.ClusterIDs); err != nil {
			return err
		}
		if err := b.repo.ReplaceQuestions(ctx, workspace, next.ID, next.QuestionIDs); err != nil {
			return err
		}
		if err := b.repo.ReplaceConnections(ctx, workspace, next.ID, next.ConnectionIDs); err != nil {
			return err
		}
		if err := b.repo.ReplaceEvents(ctx, workspace, next.ID, next.EventIDs); err != nil {
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
		clusters, err := b.repo.ClusterSnapshots(ctx, workspace, brief.ClusterIDs)
		if err != nil {
			return err
		}
		connections, err := b.repo.ConnectionSnapshots(ctx, workspace, brief.ConnectionIDs)
		if err != nil {
			return err
		}
		events, err := b.repo.EventSnapshots(ctx, workspace, brief.EventIDs)
		if err != nil {
			return err
		}
		eventRelationships, err := b.repo.EventRelationshipSnapshots(ctx, workspace, brief.EventIDs)
		if err != nil {
			return err
		}
		snapshot, err = domain.NewSnapshotWithEventsAndClustersAndRelationships(b.ids.NewID(), frozenBy, brief, clusters, questions, connections, events, eventRelationships, b.clock.Now())
		if err != nil {
			return err
		}
		if err := b.repo.CreateSnapshot(ctx, snapshot); err != nil {
			return err
		}
		if err := b.repo.ReplaceSnapshotObservations(ctx, workspace, snapshot.ID, snapshot.ObservationIDs); err != nil {
			return err
		}
		if err := b.repo.ReplaceSnapshotClusters(ctx, workspace, snapshot.ID, snapshot.Clusters); err != nil {
			return err
		}
		if err := b.repo.ReplaceSnapshotQuestions(ctx, workspace, snapshot.ID, snapshot.Questions); err != nil {
			return err
		}
		if err := b.repo.ReplaceSnapshotConnections(ctx, workspace, snapshot.ID, snapshot.Connections); err != nil {
			return err
		}
		if err := b.repo.ReplaceSnapshotEvents(ctx, workspace, snapshot.ID, snapshot.Events); err != nil {
			return err
		}
		if err := b.repo.ReplaceSnapshotEventRelationships(ctx, workspace, snapshot.ID, snapshot.EventRelationships); err != nil {
			return err
		}
		if err := b.repo.EnsureSnapshotReview(ctx, workspace, snapshot.ID, snapshot.FrozenAt); err != nil {
			return err
		}
		return b.publishSnapshot(ctx, workspace, snapshot)
	}); err != nil {
		return domain.Snapshot{}, err
	}
	return snapshot, nil
}

// SnapshotReview reads collaboration state without reading it into the frozen
// handoff. Older snapshots receive the same explicit pending default until a
// reviewer or decision creates their companion row.
func (b *Briefs) SnapshotReview(ctx context.Context, workspace, snapshot id.ID) (domain.SnapshotReview, error) {
	if workspace.IsZero() || snapshot.IsZero() {
		return domain.SnapshotReview{}, domain.ErrIDRequired
	}
	return b.repo.SnapshotReview(ctx, workspace, snapshot)
}

func (b *Briefs) AssignSnapshotReviewer(ctx context.Context, workspace, snapshot, assignedBy id.ID, assignee *id.ID) (domain.SnapshotReview, error) {
	if workspace.IsZero() || snapshot.IsZero() || assignedBy.IsZero() {
		return domain.SnapshotReview{}, domain.ErrIDRequired
	}
	if assignee != nil && assignee.IsZero() {
		return domain.SnapshotReview{}, domain.ErrIDRequired
	}
	at := b.clock.Now()
	if at.IsZero() {
		return domain.SnapshotReview{}, domain.ErrTimeRequired
	}
	var review domain.SnapshotReview
	if err := b.tx.InTx(ctx, func(ctx context.Context) error {
		if err := b.repo.EnsureSnapshotReview(ctx, workspace, snapshot, at); err != nil {
			return err
		}
		if err := b.repo.AssignSnapshotReviewer(ctx, workspace, snapshot, assignee, assignedBy, at); err != nil {
			return err
		}
		var err error
		review, err = b.repo.SnapshotReview(ctx, workspace, snapshot)
		if err != nil {
			return err
		}
		return b.publishReviewAssigned(ctx, workspace, snapshot, assignedBy, assignee)
	}); err != nil {
		return domain.SnapshotReview{}, err
	}
	return review, nil
}

func (b *Briefs) DecideSnapshotReview(ctx context.Context, workspace, snapshot, reviewer id.ID, state, note string) (domain.SnapshotReview, error) {
	if workspace.IsZero() || snapshot.IsZero() || reviewer.IsZero() {
		return domain.SnapshotReview{}, domain.ErrIDRequired
	}
	decision, err := domain.NewReviewDecision(b.ids.NewID(), workspace, snapshot, reviewer, state, note, b.clock.Now())
	if err != nil {
		return domain.SnapshotReview{}, err
	}
	var review domain.SnapshotReview
	if err := b.tx.InTx(ctx, func(ctx context.Context) error {
		if err := b.repo.EnsureSnapshotReview(ctx, workspace, snapshot, decision.CreatedAt); err != nil {
			return err
		}
		if err := b.repo.AddReviewDecision(ctx, decision); err != nil {
			return err
		}
		var err error
		review, err = b.repo.SnapshotReview(ctx, workspace, snapshot)
		if err != nil {
			return err
		}
		return b.publishReviewDecided(ctx, workspace, decision)
	}); err != nil {
		return domain.SnapshotReview{}, err
	}
	return review, nil
}

func (b *Briefs) AddSnapshotComment(ctx context.Context, workspace, snapshot, author id.ID, body string) (domain.SnapshotComment, error) {
	comment, err := domain.NewSnapshotComment(b.ids.NewID(), workspace, snapshot, author, body, b.clock.Now())
	if err != nil {
		return domain.SnapshotComment{}, err
	}
	if err := b.tx.InTx(ctx, func(ctx context.Context) error {
		if err := b.repo.CreateSnapshotComment(ctx, comment); err != nil {
			return err
		}
		return b.publishCommentCreated(ctx, workspace, comment)
	}); err != nil {
		return domain.SnapshotComment{}, err
	}
	return comment, nil
}

// CreateSnapshotShare mints an opaque, one-time-returned handle for a frozen
// recipient handoff. The database receives only the SHA-256 digest.
func (b *Briefs) CreateSnapshotShare(ctx context.Context, workspace, snapshot, createdBy id.ID) (domain.HandoffShare, string, error) {
	if workspace.IsZero() || snapshot.IsZero() || createdBy.IsZero() {
		return domain.HandoffShare{}, "", domain.ErrIDRequired
	}
	tokenID := b.ids.NewID()
	token := base64.RawURLEncoding.EncodeToString(tokenID[:])
	digest := sha256.Sum256([]byte(token))
	share, err := domain.NewHandoffShare(b.ids.NewID(), workspace, snapshot, createdBy, hex.EncodeToString(digest[:]), b.clock.Now())
	if err != nil {
		return domain.HandoffShare{}, "", err
	}
	if err := b.tx.InTx(ctx, func(ctx context.Context) error {
		if err := b.repo.CreateSnapshotShare(ctx, share); err != nil {
			return err
		}
		return b.publishShareCreated(ctx, workspace, share)
	}); err != nil {
		return domain.HandoffShare{}, "", err
	}
	return share, token, nil
}

func (b *Briefs) RevokeSnapshotShare(ctx context.Context, workspace, share, revokedBy id.ID) (domain.HandoffShare, error) {
	if workspace.IsZero() || share.IsZero() || revokedBy.IsZero() {
		return domain.HandoffShare{}, domain.ErrIDRequired
	}
	var revoked domain.HandoffShare
	if err := b.tx.InTx(ctx, func(ctx context.Context) error {
		var err error
		revoked, err = b.repo.RevokeSnapshotShare(ctx, workspace, share, revokedBy, b.clock.Now())
		if err != nil {
			return err
		}
		return b.publishShareRevoked(ctx, workspace, revoked)
	}); err != nil {
		return domain.HandoffShare{}, err
	}
	return revoked, nil
}

// RecordSnapshotHandoffAccess records a successful recipient-safe read. The
// read itself remains a query; this command only emits the accountable audit
// decision after the query has resolved.
func (b *Briefs) RecordSnapshotHandoffAccess(ctx context.Context, workspace, snapshot, accessedBy id.ID, accessMode string) error {
	if workspace.IsZero() || snapshot.IsZero() || accessedBy.IsZero() {
		return domain.ErrIDRequired
	}
	if accessMode != "direct" && accessMode != "shared" {
		return domain.ErrInvalidHandoffAccessMode
	}
	return b.publishHandoffAccess(ctx, workspace, snapshot, accessMode)
}

// RecordSnapshotHandoffExport records a successful recipient-safe export.
func (b *Briefs) RecordSnapshotHandoffExport(ctx context.Context, workspace, snapshot, exportedBy id.ID, accessMode string) error {
	if workspace.IsZero() || snapshot.IsZero() || exportedBy.IsZero() {
		return domain.ErrIDRequired
	}
	if accessMode != "direct" && accessMode != "shared" {
		return domain.ErrInvalidHandoffAccessMode
	}
	return b.publishHandoffExport(ctx, workspace, snapshot, accessMode)
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

func (b *Briefs) publishReviewAssigned(ctx context.Context, workspace, snapshot, assignedBy id.ID, assignee *id.ID) error {
	prov, ok := provenance.Current(ctx)
	if !ok {
		prov = provenance.New(provenance.OriginRequest, b.ids)
	}
	prov, err := prov.WithTenant(workspace.String())
	if err != nil {
		return err
	}
	assigneeID := ""
	if assignee != nil {
		assigneeID = assignee.String()
	}
	event, err := events.NewDecision(b.ids, b.clock, domain.EventSnapshotReviewAssigned, "workspace:"+workspace.String(), prov, domain.SnapshotReviewAssigned{WorkspaceID: workspace.String(), SnapshotID: snapshot.String(), AssignedBy: assignedBy.String(), AssigneeID: assigneeID})
	if err != nil {
		return err
	}
	return b.publisher.Publish(ctx, event)
}

func (b *Briefs) publishReviewDecided(ctx context.Context, workspace id.ID, decision domain.ReviewDecision) error {
	prov, ok := provenance.Current(ctx)
	if !ok {
		prov = provenance.New(provenance.OriginRequest, b.ids)
	}
	prov, err := prov.WithTenant(workspace.String())
	if err != nil {
		return err
	}
	event, err := events.NewDecision(b.ids, b.clock, domain.EventSnapshotReviewDecided, "workspace:"+workspace.String(), prov, domain.SnapshotReviewDecided{WorkspaceID: workspace.String(), SnapshotID: decision.SnapshotID.String(), DecisionID: decision.ID.String(), ReviewerID: decision.ReviewerID.String(), State: string(decision.State)})
	if err != nil {
		return err
	}
	return b.publisher.Publish(ctx, event)
}

func (b *Briefs) publishCommentCreated(ctx context.Context, workspace id.ID, comment domain.SnapshotComment) error {
	prov, ok := provenance.Current(ctx)
	if !ok {
		prov = provenance.New(provenance.OriginRequest, b.ids)
	}
	prov, err := prov.WithTenant(workspace.String())
	if err != nil {
		return err
	}
	event, err := events.NewDecision(b.ids, b.clock, domain.EventSnapshotCommentCreated, "workspace:"+workspace.String(), prov, domain.SnapshotCommentCreated{WorkspaceID: workspace.String(), SnapshotID: comment.SnapshotID.String(), CommentID: comment.ID.String(), AuthorID: comment.AuthorID.String()})
	if err != nil {
		return err
	}
	return b.publisher.Publish(ctx, event)
}

func (b *Briefs) publishShareCreated(ctx context.Context, workspace id.ID, share domain.HandoffShare) error {
	prov, ok := provenance.Current(ctx)
	if !ok {
		prov = provenance.New(provenance.OriginRequest, b.ids)
	}
	prov, err := prov.WithTenant(workspace.String())
	if err != nil {
		return err
	}
	event, err := events.NewDecision(b.ids, b.clock, domain.EventSnapshotShareCreated, "workspace:"+workspace.String(), prov, domain.SnapshotShareCreated{WorkspaceID: workspace.String(), SnapshotID: share.SnapshotID.String(), ShareID: share.ID.String(), CreatedBy: share.CreatedBy.String()})
	if err != nil {
		return err
	}
	return b.publisher.Publish(ctx, event)
}

func (b *Briefs) publishShareRevoked(ctx context.Context, workspace id.ID, share domain.HandoffShare) error {
	prov, ok := provenance.Current(ctx)
	if !ok {
		prov = provenance.New(provenance.OriginRequest, b.ids)
	}
	prov, err := prov.WithTenant(workspace.String())
	if err != nil {
		return err
	}
	revokedBy := ""
	if share.RevokedBy != nil {
		revokedBy = share.RevokedBy.String()
	}
	event, err := events.NewDecision(b.ids, b.clock, domain.EventSnapshotShareRevoked, "workspace:"+workspace.String(), prov, domain.SnapshotShareRevoked{WorkspaceID: workspace.String(), SnapshotID: share.SnapshotID.String(), ShareID: share.ID.String(), RevokedBy: revokedBy})
	if err != nil {
		return err
	}
	return b.publisher.Publish(ctx, event)
}

func (b *Briefs) publishHandoffAccess(ctx context.Context, workspace, snapshot id.ID, accessMode string) error {
	prov, ok := provenance.Current(ctx)
	if !ok {
		prov = provenance.New(provenance.OriginRequest, b.ids)
	}
	prov, err := prov.WithTenant(workspace.String())
	if err != nil {
		return err
	}
	event, err := events.NewDecision(b.ids, b.clock, domain.EventSnapshotHandoffAccessed, "workspace:"+workspace.String(), prov, domain.SnapshotHandoffAccessed{WorkspaceID: workspace.String(), SnapshotID: snapshot.String(), AccessMode: accessMode})
	if err != nil {
		return err
	}
	return b.publisher.Publish(ctx, event)
}

func (b *Briefs) publishHandoffExport(ctx context.Context, workspace, snapshot id.ID, accessMode string) error {
	prov, ok := provenance.Current(ctx)
	if !ok {
		prov = provenance.New(provenance.OriginRequest, b.ids)
	}
	prov, err := prov.WithTenant(workspace.String())
	if err != nil {
		return err
	}
	event, err := events.NewDecision(b.ids, b.clock, domain.EventSnapshotHandoffExported, "workspace:"+workspace.String(), prov, domain.SnapshotHandoffExported{WorkspaceID: workspace.String(), SnapshotID: snapshot.String(), AccessMode: accessMode})
	if err != nil {
		return err
	}
	return b.publisher.Publish(ctx, event)
}
