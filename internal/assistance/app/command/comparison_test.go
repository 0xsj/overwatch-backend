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

type comparisonEvidence struct{ input assistapp.ComparisonInput }

func (e comparisonEvidence) LoadComparisonInput(context.Context, id.ID, []id.ID) (assistapp.ComparisonInput, error) {
	return e.input, nil
}

type failingComparisonProvider struct{ err error }

func (p failingComparisonProvider) Compare(context.Context, assistapp.ComparisonInput) (assistapp.ComparisonOutput, error) {
	return assistapp.ComparisonOutput{}, p.err
}
func (failingComparisonProvider) Name() string            { return "external-process" }
func (failingComparisonProvider) Method() string          { return "json-selected-observations-v1" }
func (failingComparisonProvider) TemplateVersion() string { return "comparison-v1" }

type comparisonRepo struct {
	value  domain.Comparison
	events int
}

func (r *comparisonRepo) CreateComparison(_ context.Context, value domain.Comparison) error {
	r.value = value
	return nil
}
func (*comparisonRepo) CreateProviderRun(context.Context, domain.ProviderRun) error { return nil }
func (r *comparisonRepo) InTx(ctx context.Context, fn func(context.Context) error) error {
	return fn(ctx)
}
func (r *comparisonRepo) Publish(_ context.Context, evs ...events.Event) error {
	r.events += len(evs)
	return nil
}

func TestGenerateComparisonPersistsUnsupportedProviderAttempt(t *testing.T) {
	at := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)
	ids := id.NewSequence(at)
	workspace, first, second, actor := ids.NewID(), ids.NewID(), ids.NewID(), ids.NewID()
	repo := &comparisonRepo{}
	service := command.NewComparisons(repo, comparisonEvidence{input: assistapp.ComparisonInput{Observations: []assistapp.Observation{{ID: first}, {ID: second}}}}, failingComparisonProvider{err: assistapp.ErrProcessProviderUnavailable}, repo, repo, ids, clock.NewFake(at))
	found, err := service.Generate(context.Background(), workspace, []id.ID{first, second}, actor)
	if !errors.Is(err, assistapp.ErrProcessProviderUnavailable) {
		t.Fatalf("error=%v, want unavailable provider error", err)
	}
	if found.ID.IsZero() || found.Status != domain.ComparisonUnsupported || found.Error == "" || repo.value.ID != found.ID || repo.events != 1 {
		t.Fatalf("unsupported comparison attempt was not retained: found=%+v stored=%+v events=%d", found, repo.value, repo.events)
	}
}

func TestGenerateComparisonPersistsPolicyBlockedExternalAttempt(t *testing.T) {
	at := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)
	ids := id.NewSequence(at)
	workspace, first, second, actor := ids.NewID(), ids.NewID(), ids.NewID(), ids.NewID()
	repo := &comparisonRepo{}
	service := command.NewComparisonsWithPolicy(repo, comparisonEvidence{input: assistapp.ComparisonInput{Observations: []assistapp.Observation{{ID: first}, {ID: second}}}}, failingComparisonProvider{err: assistapp.ErrProcessProviderUnavailable}, deniedSynthesisProviderPolicy{}, repo, repo, ids, clock.NewFake(at))
	found, err := service.Generate(context.Background(), workspace, []id.ID{first, second}, actor)
	if !errors.Is(err, domain.ErrExternalProviderDisabled) {
		t.Fatalf("error=%v, want external-provider policy error", err)
	}
	if found.ID.IsZero() || found.Status != domain.ComparisonUnsupported || found.Error == "" || repo.value.ID != found.ID || repo.events != 1 {
		t.Fatalf("policy-blocked comparison attempt was not retained: found=%+v stored=%+v events=%d", found, repo.value, repo.events)
	}
}
