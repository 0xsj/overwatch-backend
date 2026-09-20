package command

import (
	"context"

	assistapp "github.com/0xsj/overwatch-backend/internal/assistance/app"
	"github.com/0xsj/overwatch-backend/internal/assistance/domain"
	connectiondomain "github.com/0xsj/overwatch-backend/internal/researchconnection/domain"
	"github.com/0xsj/overwatch-backend/pkg/events"
	"github.com/0xsj/overwatch-backend/pkg/id"
	"github.com/0xsj/overwatch-backend/pkg/provenance"
)

type ConnectionReviewRepository interface {
	CreateConnectionReview(context.Context, domain.ConnectionReview) error
}

type ConnectionReviewConnectionReader interface {
	ByID(context.Context, id.ID, id.ID) (connectiondomain.Connection, error)
}

type ConnectionReviewEvidence interface {
	LoadConnectionReviewInput(context.Context, connectiondomain.Connection) (assistapp.ConnectionReviewInput, error)
}

type ConnectionReviews struct {
	repo        ConnectionReviewRepository
	connections ConnectionReviewConnectionReader
	evidence    ConnectionReviewEvidence
	provider    assistapp.ConnectionReviewProvider
	tx          Transactor
	publisher   events.Publisher
	ids         Minter
	clock       Clock
}

func NewConnectionReviews(repo ConnectionReviewRepository, connections ConnectionReviewConnectionReader, evidence ConnectionReviewEvidence, provider assistapp.ConnectionReviewProvider, tx Transactor, publisher events.Publisher, ids Minter, clock Clock) *ConnectionReviews {
	if repo == nil || connections == nil || evidence == nil || provider == nil || tx == nil || publisher == nil || ids == nil || clock == nil {
		panic("assistance: NewConnectionReviews with a nil dependency")
	}
	return &ConnectionReviews{repo: repo, connections: connections, evidence: evidence, provider: provider, tx: tx, publisher: publisher, ids: ids, clock: clock}
}

func (c *ConnectionReviews) Generate(ctx context.Context, workspace, connectionID, actor id.ID) (domain.ConnectionReview, error) {
	if workspace.IsZero() || connectionID.IsZero() || actor.IsZero() {
		return domain.ConnectionReview{}, domain.ErrIDRequired
	}
	connection, err := c.connections.ByID(ctx, workspace, connectionID)
	if err != nil {
		return domain.ConnectionReview{}, err
	}
	input, err := c.evidence.LoadConnectionReviewInput(ctx, connection)
	if err != nil {
		return domain.ConnectionReview{}, err
	}
	if len(input.SupportingObservationIDs)+len(input.OpposingObservationIDs) == 0 {
		return domain.ConnectionReview{}, domain.ErrConnectionReviewRequired
	}
	out, err := c.provider.ReviewConnection(ctx, input)
	if err != nil {
		return domain.ConnectionReview{}, err
	}
	fresh, err := domain.NewConnectionReview(c.ids.NewID(), workspace, actor, connection.ID, connection.FromRecordID, connection.ToRecordID, connection.Kind.String(), connection.State.String(), connection.Rationale, connection.SupportingObservationIDs, connection.OpposingObservationIDs, c.provider.Name(), c.provider.Method(), c.provider.TemplateVersion(), out.Status, out.Text, out.Findings, c.clock.Now())
	if err != nil {
		return domain.ConnectionReview{}, err
	}
	if err := c.tx.InTx(ctx, func(ctx context.Context) error {
		if err := c.repo.CreateConnectionReview(ctx, fresh); err != nil {
			return err
		}
		prov, ok := provenance.Current(ctx)
		if !ok {
			prov = provenance.New(provenance.OriginRequest, c.ids)
		}
		prov, err := prov.WithTenant(workspace.String())
		if err != nil {
			return err
		}
		event, err := events.NewDecision(c.ids, c.clock, domain.EventConnectionReviewGenerated, "workspace:"+workspace.String(), prov, map[string]any{
			"workspace_id": workspace.String(), "connection_id": fresh.ConnectionID.String(), "connection_review_id": fresh.ID.String(), "finding_count": len(fresh.Findings), "provider": fresh.Provider, "method": fresh.Method, "template_version": fresh.TemplateVersion, "status": fresh.Status.String(), "actor": actor.String(),
		})
		if err != nil {
			return err
		}
		return c.publisher.Publish(ctx, event)
	}); err != nil {
		return domain.ConnectionReview{}, err
	}
	return fresh, nil
}
