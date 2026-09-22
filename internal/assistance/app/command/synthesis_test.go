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

type synthesisEvidence struct {
	rows map[id.ID]assistapp.Observation
}

func (s synthesisEvidence) Evidence(_ context.Context, _ id.ID, observation id.ID) (assistapp.Observation, error) {
	return s.rows[observation], nil
}

type synthesisRepo struct {
	value  domain.Synthesis
	events int
}

type failingSynthesisProvider struct{ err error }

func (p failingSynthesisProvider) Synthesize(context.Context, []assistapp.Observation) (assistapp.SynthesisOutput, error) {
	return assistapp.SynthesisOutput{}, p.err
}
func (failingSynthesisProvider) Name() string   { return "external-process" }
func (failingSynthesisProvider) Method() string { return "json-selected-observations-v1" }

type deniedSynthesisProviderPolicy struct{}

func (deniedSynthesisProviderPolicy) Current(_ context.Context, workspace id.ID) (domain.ProviderPolicy, error) {
	return domain.DefaultProviderPolicy(workspace), nil
}

func (s *synthesisRepo) CreateSynthesis(_ context.Context, value domain.Synthesis) error {
	s.value = value
	return nil
}
func (*synthesisRepo) CreateProviderRun(context.Context, domain.ProviderRun) error { return nil }
func (s *synthesisRepo) InTx(ctx context.Context, fn func(context.Context) error) error {
	return fn(ctx)
}
func (s *synthesisRepo) Publish(_ context.Context, evs ...events.Event) error {
	s.events += len(evs)
	return nil
}

func TestGenerateSynthesisPersistsAResumableReviewAid(t *testing.T) {
	at := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)
	ids := id.NewSequence(at)
	workspace, first, second, actor := ids.NewID(), ids.NewID(), ids.NewID(), ids.NewID()
	repo := &synthesisRepo{}
	service := command.NewSyntheses(repo, synthesisEvidence{rows: map[id.ID]assistapp.Observation{
		first:  {ID: first, WorkspaceID: workspace, SourceTitle: "Notice A", Statement: "The account @HarborLine posted.", Quote: "@HarborLine"},
		second: {ID: second, WorkspaceID: workspace, SourceTitle: "Notice B", Statement: "Contact user@example.com.", Quote: "user@example.com"},
	}}, assistapp.LocalSynthesisProvider{}, repo, repo, ids, clock.NewFake(at))
	found, err := service.Generate(context.Background(), workspace, []id.ID{first, second}, actor)
	if err != nil {
		t.Fatal(err)
	}
	if found.ID != repo.value.ID || found.Provider != "local" || found.Method != "selected-observations-v1" || len(found.Candidates) != 2 || repo.events != 1 {
		t.Fatalf("generated synthesis=%+v stored=%+v events=%d", found, repo.value, repo.events)
	}
}

func TestGenerateSynthesisPersistsUnsupportedProviderAttempt(t *testing.T) {
	at := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)
	ids := id.NewSequence(at)
	workspace, observation, actor := ids.NewID(), ids.NewID(), ids.NewID()
	repo := &synthesisRepo{}
	service := command.NewSyntheses(repo, synthesisEvidence{rows: map[id.ID]assistapp.Observation{
		observation: {ID: observation, WorkspaceID: workspace, SourceTitle: "Notice", Statement: "The account @HarborLine posted."},
	}}, failingSynthesisProvider{err: assistapp.ErrSynthesisProviderUnavailable}, repo, repo, ids, clock.NewFake(at))
	found, err := service.Generate(context.Background(), workspace, []id.ID{observation}, actor)
	if !errors.Is(err, assistapp.ErrSynthesisProviderUnavailable) {
		t.Fatalf("error=%v, want unavailable provider error", err)
	}
	if found.ID.IsZero() || found.Status != domain.SynthesisUnsupported || found.Error == "" || repo.value.ID != found.ID || repo.events != 1 {
		t.Fatalf("unsupported synthesis attempt was not retained: found=%+v stored=%+v events=%d", found, repo.value, repo.events)
	}
}

func TestGenerateSynthesisPersistsPolicyBlockedExternalAttempt(t *testing.T) {
	at := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)
	ids := id.NewSequence(at)
	workspace, observation, actor := ids.NewID(), ids.NewID(), ids.NewID()
	repo := &synthesisRepo{}
	service := command.NewSynthesesWithPolicy(repo, synthesisEvidence{rows: map[id.ID]assistapp.Observation{
		observation: {ID: observation, WorkspaceID: workspace, SourceTitle: "Notice", Statement: "The account @HarborLine posted."},
	}}, failingSynthesisProvider{err: assistapp.ErrSynthesisProviderUnavailable}, deniedSynthesisProviderPolicy{}, repo, repo, ids, clock.NewFake(at))
	found, err := service.Generate(context.Background(), workspace, []id.ID{observation}, actor)
	if !errors.Is(err, domain.ErrExternalProviderDisabled) {
		t.Fatalf("error=%v, want external-provider policy error", err)
	}
	if found.ID.IsZero() || found.Status != domain.SynthesisUnsupported || found.Error == "" || repo.value.ID != found.ID || repo.events != 1 {
		t.Fatalf("policy-blocked synthesis attempt was not retained: found=%+v stored=%+v events=%d", found, repo.value, repo.events)
	}
}
