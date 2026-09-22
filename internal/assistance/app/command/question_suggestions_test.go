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

type questionSuggestionEvidence struct {
	input assistapp.QuestionSuggestionInput
}

func (e questionSuggestionEvidence) LoadQuestionSuggestionInput(context.Context, id.ID, []domain.QuestionSuggestionGap) (assistapp.QuestionSuggestionInput, error) {
	return e.input, nil
}

type failingQuestionSuggestionProvider struct{ err error }

func (p failingQuestionSuggestionProvider) SuggestQuestions(context.Context, assistapp.QuestionSuggestionInput) (assistapp.QuestionSuggestionOutput, error) {
	return assistapp.QuestionSuggestionOutput{}, p.err
}
func (failingQuestionSuggestionProvider) Name() string            { return "external-process" }
func (failingQuestionSuggestionProvider) Method() string          { return "json-question-suggestions-v1" }
func (failingQuestionSuggestionProvider) TemplateVersion() string { return "question-suggestions-v1" }

type questionSuggestionRepo struct {
	value  domain.QuestionSuggestions
	events int
}

func (r *questionSuggestionRepo) CreateQuestionSuggestions(_ context.Context, value domain.QuestionSuggestions) error {
	r.value = value
	return nil
}
func (*questionSuggestionRepo) CreateProviderRun(context.Context, domain.ProviderRun) error {
	return nil
}
func (r *questionSuggestionRepo) InTx(ctx context.Context, fn func(context.Context) error) error {
	return fn(ctx)
}
func (r *questionSuggestionRepo) Publish(_ context.Context, evs ...events.Event) error {
	r.events += len(evs)
	return nil
}

func TestGenerateQuestionSuggestionsPersistsUnsupportedProviderAttempt(t *testing.T) {
	at := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)
	ids := id.NewSequence(at)
	workspace, observation, actor := ids.NewID(), ids.NewID(), ids.NewID()
	gaps := []domain.QuestionSuggestionGap{{Kind: domain.QuestionGapCorroboration, Label: "the account", Detail: "independent support is missing", ObservationIDs: []id.ID{observation}}}
	repo := &questionSuggestionRepo{}
	service := command.NewQuestionSuggestions(repo, questionSuggestionEvidence{}, failingQuestionSuggestionProvider{err: assistapp.ErrProcessProviderUnavailable}, repo, repo, ids, clock.NewFake(at))
	found, err := service.Generate(context.Background(), workspace, gaps, actor)
	if !errors.Is(err, assistapp.ErrProcessProviderUnavailable) {
		t.Fatalf("error=%v, want unavailable provider error", err)
	}
	if found.ID.IsZero() || found.Status != domain.QuestionSuggestionsUnsupported || found.Error == "" || repo.value.ID != found.ID || repo.events != 1 {
		t.Fatalf("unsupported question suggestion attempt was not retained: found=%+v stored=%+v events=%d", found, repo.value, repo.events)
	}
}

func TestGenerateQuestionSuggestionsPersistsPolicyBlockedExternalAttempt(t *testing.T) {
	at := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)
	ids := id.NewSequence(at)
	workspace, observation, actor := ids.NewID(), ids.NewID(), ids.NewID()
	gaps := []domain.QuestionSuggestionGap{{Kind: domain.QuestionGapCorroboration, Label: "the account", Detail: "independent support is missing", ObservationIDs: []id.ID{observation}}}
	repo := &questionSuggestionRepo{}
	service := command.NewQuestionSuggestionsWithPolicy(repo, questionSuggestionEvidence{}, failingQuestionSuggestionProvider{err: assistapp.ErrProcessProviderUnavailable}, deniedSynthesisProviderPolicy{}, repo, repo, ids, clock.NewFake(at))
	found, err := service.Generate(context.Background(), workspace, gaps, actor)
	if !errors.Is(err, domain.ErrExternalProviderDisabled) {
		t.Fatalf("error=%v, want external-provider policy error", err)
	}
	if found.ID.IsZero() || found.Status != domain.QuestionSuggestionsUnsupported || found.Error == "" || repo.value.ID != found.ID || repo.events != 1 {
		t.Fatalf("policy-blocked question-suggestion attempt was not retained: found=%+v stored=%+v events=%d", found, repo.value, repo.events)
	}
}
