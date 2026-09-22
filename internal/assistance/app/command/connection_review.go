package command

import (
	"context"
	"strings"
	"unicode/utf8"

	assistapp "github.com/0xsj/overwatch-backend/internal/assistance/app"
	"github.com/0xsj/overwatch-backend/internal/assistance/domain"
	connectiondomain "github.com/0xsj/overwatch-backend/internal/researchconnection/domain"
	pkgerrors "github.com/0xsj/overwatch-backend/pkg/errors"
	"github.com/0xsj/overwatch-backend/pkg/events"
	"github.com/0xsj/overwatch-backend/pkg/id"
	"github.com/0xsj/overwatch-backend/pkg/provenance"
)

type ConnectionReviewRepository interface {
	CreateConnectionReview(context.Context, domain.ConnectionReview) error
	ProviderRunRepository
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
	policy      ProviderPolicy
	tx          Transactor
	publisher   events.Publisher
	ids         Minter
	clock       Clock
}

func NewConnectionReviews(repo ConnectionReviewRepository, connections ConnectionReviewConnectionReader, evidence ConnectionReviewEvidence, provider assistapp.ConnectionReviewProvider, tx Transactor, publisher events.Publisher, ids Minter, clock Clock) *ConnectionReviews {
	if repo == nil || connections == nil || evidence == nil || provider == nil || tx == nil || publisher == nil || ids == nil || clock == nil {
		panic("assistance: NewConnectionReviews with a nil dependency")
	}
	return &ConnectionReviews{repo: repo, connections: connections, evidence: evidence, provider: provider, policy: allowAllProviderPolicy{}, tx: tx, publisher: publisher, ids: ids, clock: clock}
}

func NewConnectionReviewsWithPolicy(repo ConnectionReviewRepository, connections ConnectionReviewConnectionReader, evidence ConnectionReviewEvidence, provider assistapp.ConnectionReviewProvider, policy ProviderPolicy, tx Transactor, publisher events.Publisher, ids Minter, clock Clock) *ConnectionReviews {
	if repo == nil || connections == nil || evidence == nil || provider == nil || policy == nil || tx == nil || publisher == nil || ids == nil || clock == nil {
		panic("assistance: NewConnectionReviewsWithPolicy with a nil dependency")
	}
	return &ConnectionReviews{repo: repo, connections: connections, evidence: evidence, provider: provider, policy: policy, tx: tx, publisher: publisher, ids: ids, clock: clock}
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
	timer := startProviderRun(input)
	if providerUsesExternal(c.provider) {
		policy, err := c.policy.Current(ctx, workspace)
		if err != nil {
			return domain.ConnectionReview{}, err
		}
		if !policy.AllowExternal {
			failed, buildErr := domain.NewConnectionReviewResult(c.ids.NewID(), workspace, actor, connection.ID, connection.FromRecordID, connection.ToRecordID, connection.Kind.String(), connection.State.String(), connection.Rationale, connection.SupportingObservationIDs, connection.OpposingObservationIDs, c.provider.Name(), c.provider.Method(), c.provider.TemplateVersion(), domain.ConnectionReviewUnsupported, "", nil, domain.ErrExternalProviderDisabled.Error(), c.clock.Now())
			if buildErr != nil {
				return domain.ConnectionReview{}, buildErr
			}
			run, runErr := timer.finish(failed.ID, workspace, actor, "connection_review", failed.Provider, failed.Method, failed.TemplateVersion, failed.Status.String(), failed.Output, failed.Error, false, failed.CreatedAt)
			if runErr != nil {
				return domain.ConnectionReview{}, runErr
			}
			if persistErr := c.persist(ctx, failed, run, actor); persistErr != nil {
				return domain.ConnectionReview{}, persistErr
			}
			return failed, domain.ErrExternalProviderDisabled
		}
	}
	out, err := c.provider.ReviewConnection(ctx, input)
	if err != nil {
		status := domain.ConnectionReviewFailed
		if pkgerrors.KindOf(err) == pkgerrors.Unavailable {
			status = domain.ConnectionReviewUnsupported
		} else if pkgerrors.KindOf(err) == pkgerrors.Timeout {
			status = domain.ConnectionReviewTimedOut
		}
		failed, buildErr := domain.NewConnectionReviewResult(c.ids.NewID(), workspace, actor, connection.ID, connection.FromRecordID, connection.ToRecordID, connection.Kind.String(), connection.State.String(), connection.Rationale, connection.SupportingObservationIDs, connection.OpposingObservationIDs, c.provider.Name(), c.provider.Method(), c.provider.TemplateVersion(), status, "", nil, trimConnectionReviewError(err.Error()), c.clock.Now())
		if buildErr != nil {
			return domain.ConnectionReview{}, buildErr
		}
		run, runErr := timer.finish(failed.ID, workspace, actor, "connection_review", failed.Provider, failed.Method, failed.TemplateVersion, failed.Status.String(), failed.Output, failed.Error, status == domain.ConnectionReviewTimedOut, failed.CreatedAt)
		if runErr != nil {
			return domain.ConnectionReview{}, runErr
		}
		if persistErr := c.persist(ctx, failed, run, actor); persistErr != nil {
			return domain.ConnectionReview{}, persistErr
		}
		return failed, err
	}
	fresh, err := domain.NewConnectionReview(c.ids.NewID(), workspace, actor, connection.ID, connection.FromRecordID, connection.ToRecordID, connection.Kind.String(), connection.State.String(), connection.Rationale, connection.SupportingObservationIDs, connection.OpposingObservationIDs, c.provider.Name(), c.provider.Method(), c.provider.TemplateVersion(), out.Status, out.Text, out.Findings, c.clock.Now())
	if err != nil {
		return domain.ConnectionReview{}, err
	}
	run, err := timer.finish(fresh.ID, workspace, actor, "connection_review", fresh.Provider, fresh.Method, fresh.TemplateVersion, fresh.Status.String(), fresh.Output, fresh.Error, false, fresh.CreatedAt)
	if err != nil {
		return domain.ConnectionReview{}, err
	}
	if err := c.persist(ctx, fresh, run, actor); err != nil {
		return domain.ConnectionReview{}, err
	}
	return fresh, nil
}

func (c *ConnectionReviews) persist(ctx context.Context, fresh domain.ConnectionReview, run domain.ProviderRun, actor id.ID) error {
	return c.tx.InTx(ctx, func(ctx context.Context) error {
		if err := c.repo.CreateConnectionReview(ctx, fresh); err != nil {
			return err
		}
		if err := c.repo.CreateProviderRun(ctx, run); err != nil {
			return err
		}
		prov, ok := provenance.Current(ctx)
		if !ok {
			prov = provenance.New(provenance.OriginRequest, c.ids)
		}
		prov, err := prov.WithTenant(fresh.WorkspaceID.String())
		if err != nil {
			return err
		}
		event, err := events.NewDecision(c.ids, c.clock, domain.EventConnectionReviewGenerated, "workspace:"+fresh.WorkspaceID.String(), prov, map[string]any{
			"workspace_id": fresh.WorkspaceID.String(), "connection_id": fresh.ConnectionID.String(), "connection_review_id": fresh.ID.String(), "finding_count": len(fresh.Findings), "provider": fresh.Provider, "method": fresh.Method, "template_version": fresh.TemplateVersion, "status": fresh.Status.String(), "error": fresh.Error, "actor": actor.String(),
		})
		if err != nil {
			return err
		}
		return c.publisher.Publish(ctx, event)
	})
}

func trimConnectionReviewError(raw string) string {
	failure := strings.TrimSpace(raw)
	if len(failure) <= domain.MaxConnectionReviewError {
		return failure
	}
	failure = failure[:domain.MaxConnectionReviewError]
	for !utf8.ValidString(failure) {
		failure = failure[:len(failure)-1]
	}
	return failure
}
