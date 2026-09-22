package command_test

import (
	"context"
	"errors"
	"testing"
	"time"

	assistapp "github.com/0xsj/overwatch-backend/internal/assistance/app"
	"github.com/0xsj/overwatch-backend/internal/assistance/app/command"
	"github.com/0xsj/overwatch-backend/internal/assistance/domain"
	connectiondomain "github.com/0xsj/overwatch-backend/internal/researchconnection/domain"
	"github.com/0xsj/overwatch-backend/pkg/clock"
	"github.com/0xsj/overwatch-backend/pkg/events"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

type connectionReviewConnections struct{ value connectiondomain.Connection }

func (r connectionReviewConnections) ByID(context.Context, id.ID, id.ID) (connectiondomain.Connection, error) {
	return r.value, nil
}

type connectionReviewEvidence struct {
	input assistapp.ConnectionReviewInput
}

func (e connectionReviewEvidence) LoadConnectionReviewInput(context.Context, connectiondomain.Connection) (assistapp.ConnectionReviewInput, error) {
	return e.input, nil
}

type failingConnectionReviewProvider struct{ err error }

func (p failingConnectionReviewProvider) ReviewConnection(context.Context, assistapp.ConnectionReviewInput) (assistapp.ConnectionReviewOutput, error) {
	return assistapp.ConnectionReviewOutput{}, p.err
}
func (failingConnectionReviewProvider) Name() string            { return "external-process" }
func (failingConnectionReviewProvider) Method() string          { return "json-connection-review-v1" }
func (failingConnectionReviewProvider) TemplateVersion() string { return "connection-review-v1" }

type connectionReviewRepo struct {
	value  domain.ConnectionReview
	events int
}

func (r *connectionReviewRepo) CreateConnectionReview(_ context.Context, value domain.ConnectionReview) error {
	r.value = value
	return nil
}
func (*connectionReviewRepo) CreateProviderRun(context.Context, domain.ProviderRun) error { return nil }
func (r *connectionReviewRepo) InTx(ctx context.Context, fn func(context.Context) error) error {
	return fn(ctx)
}
func (r *connectionReviewRepo) Publish(_ context.Context, evs ...events.Event) error {
	r.events += len(evs)
	return nil
}

func TestGenerateConnectionReviewPersistsUnsupportedProviderAttempt(t *testing.T) {
	at := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)
	ids := id.NewSequence(at)
	workspace, connectionID, from, to, observation, actor := ids.NewID(), ids.NewID(), ids.NewID(), ids.NewID(), ids.NewID(), ids.NewID()
	connection := connectiondomain.Connection{ID: connectionID, WorkspaceID: workspace, FromRecordID: from, ToRecordID: to, Kind: connectiondomain.AssociatedWith, State: connectiondomain.Proposed, Rationale: "review the authored relationship", SupportingObservationIDs: []id.ID{observation}}
	repo := &connectionReviewRepo{}
	service := command.NewConnectionReviews(repo, connectionReviewConnections{value: connection}, connectionReviewEvidence{input: assistapp.ConnectionReviewInput{SupportingObservationIDs: []id.ID{observation}}}, failingConnectionReviewProvider{err: assistapp.ErrProcessProviderUnavailable}, repo, repo, ids, clock.NewFake(at))
	found, err := service.Generate(context.Background(), workspace, connectionID, actor)
	if !errors.Is(err, assistapp.ErrProcessProviderUnavailable) {
		t.Fatalf("error=%v, want unavailable provider error", err)
	}
	if found.ID.IsZero() || found.Status != domain.ConnectionReviewUnsupported || found.Error == "" || repo.value.ID != found.ID || repo.events != 1 {
		t.Fatalf("unsupported connection review attempt was not retained: found=%+v stored=%+v events=%d", found, repo.value, repo.events)
	}
}

func TestGenerateConnectionReviewPersistsPolicyBlockedExternalAttempt(t *testing.T) {
	at := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)
	ids := id.NewSequence(at)
	workspace, connectionID, from, to, observation, actor := ids.NewID(), ids.NewID(), ids.NewID(), ids.NewID(), ids.NewID(), ids.NewID()
	connection := connectiondomain.Connection{ID: connectionID, WorkspaceID: workspace, FromRecordID: from, ToRecordID: to, Kind: connectiondomain.AssociatedWith, State: connectiondomain.Proposed, Rationale: "review the authored relationship", SupportingObservationIDs: []id.ID{observation}}
	repo := &connectionReviewRepo{}
	service := command.NewConnectionReviewsWithPolicy(repo, connectionReviewConnections{value: connection}, connectionReviewEvidence{input: assistapp.ConnectionReviewInput{SupportingObservationIDs: []id.ID{observation}}}, failingConnectionReviewProvider{err: assistapp.ErrProcessProviderUnavailable}, deniedSynthesisProviderPolicy{}, repo, repo, ids, clock.NewFake(at))
	found, err := service.Generate(context.Background(), workspace, connectionID, actor)
	if !errors.Is(err, domain.ErrExternalProviderDisabled) {
		t.Fatalf("error=%v, want external-provider policy error", err)
	}
	if found.ID.IsZero() || found.Status != domain.ConnectionReviewUnsupported || found.Error == "" || repo.value.ID != found.ID || repo.events != 1 {
		t.Fatalf("policy-blocked connection review attempt was not retained: found=%+v stored=%+v events=%d", found, repo.value, repo.events)
	}
}
