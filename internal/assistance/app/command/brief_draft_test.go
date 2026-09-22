package command_test

import (
	"context"
	"errors"
	"testing"
	"time"

	assistapp "github.com/0xsj/overwatch-backend/internal/assistance/app"
	"github.com/0xsj/overwatch-backend/internal/assistance/app/command"
	"github.com/0xsj/overwatch-backend/internal/assistance/domain"
	"github.com/0xsj/overwatch-backend/pkg/clock"
	"github.com/0xsj/overwatch-backend/pkg/events"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

type briefDraftEvidence struct{ input assistapp.BriefDraftInput }

func (e briefDraftEvidence) LoadBriefDraftInput(context.Context, id.ID, []id.ID) (assistapp.BriefDraftInput, error) {
	return e.input, nil
}

type failingBriefDraftProvider struct{ err error }

func (p failingBriefDraftProvider) DraftBrief(context.Context, assistapp.BriefDraftInput) (domain.BriefDraftStatus, string, []domain.BriefDraftChange, error) {
	return "", "", nil, p.err
}
func (failingBriefDraftProvider) Name() string            { return "external-process" }
func (failingBriefDraftProvider) Method() string          { return "json-brief-draft-v1" }
func (failingBriefDraftProvider) TemplateVersion() string { return "brief-draft-v1" }

type briefDraftRepo struct {
	value  domain.BriefDraft
	events int
}

func (r *briefDraftRepo) CreateBriefDraft(_ context.Context, value domain.BriefDraft) error {
	r.value = value
	return nil
}
func (*briefDraftRepo) CreateProviderRun(context.Context, domain.ProviderRun) error { return nil }
func (r *briefDraftRepo) InTx(ctx context.Context, fn func(context.Context) error) error {
	return fn(ctx)
}
func (r *briefDraftRepo) Publish(_ context.Context, evs ...events.Event) error {
	r.events += len(evs)
	return nil
}

func TestGenerateBriefDraftPersistsUnsupportedProviderAttempt(t *testing.T) {
	at := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)
	ids := id.NewSequence(at)
	workspace, brief, observation, actor := ids.NewID(), ids.NewID(), ids.NewID(), ids.NewID()
	repo := &briefDraftRepo{}
	service := command.NewBriefDrafts(repo, briefDraftEvidence{input: assistapp.BriefDraftInput{
		Brief:        domain.BriefDraftInput{BriefID: brief, Title: "East Quay", Question: "What happened?", ObservationIDs: []id.ID{observation}},
		Observations: []assistapp.BriefDraftObservation{{ID: observation, SourceTitle: "Notice", Statement: "A disruption was reported."}},
	}}, failingBriefDraftProvider{err: assistapp.ErrProcessProviderUnavailable}, repo, repo, ids, clock.NewFake(at))
	found, err := service.Generate(context.Background(), workspace, []id.ID{observation}, actor)
	if !errors.Is(err, assistapp.ErrProcessProviderUnavailable) {
		t.Fatalf("error=%v, want unavailable provider error", err)
	}
	if found.ID.IsZero() || found.Status != domain.BriefDraftUnsupported || found.Error == "" || found.Input.BriefID != brief || repo.value.ID != found.ID || repo.events != 1 {
		t.Fatalf("unsupported brief draft attempt was not retained: found=%+v stored=%+v events=%d", found, repo.value, repo.events)
	}
}

func TestGenerateBriefDraftPersistsPolicyBlockedExternalAttempt(t *testing.T) {
	at := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)
	ids := id.NewSequence(at)
	workspace, brief, observation, actor := ids.NewID(), ids.NewID(), ids.NewID(), ids.NewID()
	repo := &briefDraftRepo{}
	service := command.NewBriefDraftsWithPolicy(repo, briefDraftEvidence{input: assistapp.BriefDraftInput{
		Brief:        domain.BriefDraftInput{BriefID: brief, Title: "East Quay", Question: "What happened?", ObservationIDs: []id.ID{observation}},
		Observations: []assistapp.BriefDraftObservation{{ID: observation, SourceTitle: "Notice", Statement: "A disruption was reported."}},
	}}, failingBriefDraftProvider{err: assistapp.ErrProcessProviderUnavailable}, deniedSynthesisProviderPolicy{}, repo, repo, ids, clock.NewFake(at))
	found, err := service.Generate(context.Background(), workspace, []id.ID{observation}, actor)
	if !errors.Is(err, domain.ErrExternalProviderDisabled) {
		t.Fatalf("error=%v, want external-provider policy error", err)
	}
	if found.ID.IsZero() || found.Status != domain.BriefDraftUnsupported || found.Error == "" || repo.value.ID != found.ID || repo.events != 1 {
		t.Fatalf("policy-blocked brief draft attempt was not retained: found=%+v stored=%+v events=%d", found, repo.value, repo.events)
	}
}
