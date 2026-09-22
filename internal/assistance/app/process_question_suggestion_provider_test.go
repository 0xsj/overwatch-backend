package app_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/0xsj/overwatch-backend/internal/assistance/app"
	"github.com/0xsj/overwatch-backend/internal/assistance/domain"
	pkgerrors "github.com/0xsj/overwatch-backend/pkg/errors"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

func TestProcessQuestionSuggestionProviderReadsGroundedJSON(t *testing.T) {
	observation := id.ID{1}
	provider := app.NewProcessQuestionSuggestionProvider(app.QuestionSuggestionsProcessProviderConfig{
		Binary: "/bin/sh",
		Args:   []string{"-c", fmt.Sprintf(`grep -q '"label":"the account"' "$1" && printf '{"status":"completed","text":"Seek corroboration.","suggestions":[{"kind":"corroboration_gap","prompt":"Which independent source would corroborate the account?","context":"The current account has one citation.","observation_ids":["%s"]}]}'`, observation.String()), "question-test", app.QuestionSuggestionsInputPlaceholder},
	})
	found, err := provider.SuggestQuestions(context.Background(), app.QuestionSuggestionInput{Gaps: []app.QuestionSuggestionGapInput{{
		Kind: domain.QuestionGapCorroboration, Label: "the account", Detail: "one citation", ObservationIDs: []id.ID{observation},
		Observations: []app.Observation{{ID: observation, SourceTitle: "Notice", Statement: "The account was named."}},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	if found.Status.String() != "completed" || found.Text == "" || len(found.Suggestions) != 1 || found.Suggestions[0].ObservationIDs[0] != observation {
		t.Fatalf("unexpected question suggestions: %+v", found)
	}
}

func TestProcessQuestionSuggestionProviderRejectsUnknownFields(t *testing.T) {
	provider := app.NewProcessQuestionSuggestionProvider(app.QuestionSuggestionsProcessProviderConfig{Binary: "/bin/sh", Args: []string{"-c", "printf '%s' \"$1\"", "question-test", `{"status":"completed","text":"review","suggestions":[],"extra":true}`, app.QuestionSuggestionsInputPlaceholder}, MaxOutput: 1024})
	_, err := provider.SuggestQuestions(context.Background(), app.QuestionSuggestionInput{Gaps: []app.QuestionSuggestionGapInput{{Kind: domain.QuestionGapOpenQuestion, Label: "the account", ObservationIDs: []id.ID{{1}}}}})
	if err == nil {
		t.Fatal("expected provider output to be rejected")
	}
}

func TestProcessQuestionSuggestionProviderMissingBinaryIsExplicitlyUnavailable(t *testing.T) {
	provider := app.NewProcessQuestionSuggestionProvider(app.QuestionSuggestionsProcessProviderConfig{Binary: "not-a-real-overwatch-question-provider"})
	_, err := provider.SuggestQuestions(context.Background(), app.QuestionSuggestionInput{Gaps: []app.QuestionSuggestionGapInput{{Kind: domain.QuestionGapOpenQuestion, Label: "the account", ObservationIDs: []id.ID{{1}}}}})
	if !errors.Is(err, app.ErrProcessProviderUnavailable) {
		t.Fatalf("error=%v, want unavailable provider error", err)
	}
	if got := pkgerrors.KindOf(err); got != pkgerrors.Unavailable {
		t.Fatalf("error kind=%s, want unavailable", got)
	}
}
