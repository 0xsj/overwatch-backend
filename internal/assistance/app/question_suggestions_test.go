package app

import (
	"context"
	"testing"

	"github.com/0xsj/overwatch-backend/internal/assistance/domain"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

func TestLocalQuestionSuggestionProviderKeepsPromptsGroundedInSelectedGaps(t *testing.T) {
	left := id.ID{1}
	right := id.ID{2}
	out, err := (LocalQuestionSuggestionProvider{}).SuggestQuestions(context.Background(), QuestionSuggestionInput{
		Gaps: []QuestionSuggestionGapInput{
			{Kind: domain.QuestionGapContradiction, Label: "East Quay opening status", Detail: "The two notices disagree about whether the venue was open.", ObservationIDs: []id.ID{left, right}},
			{Kind: domain.QuestionGapCorroboration, Label: "East Quay location", Detail: "Both accounts need an independent source.", ObservationIDs: []id.ID{left}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.Status != domain.QuestionSuggestionsCompleted || out.Text == "" || len(out.Suggestions) != 2 {
		t.Fatalf("question suggestion output: %+v", out)
	}
	for _, suggestion := range out.Suggestions {
		if suggestion.Prompt == "" || len(suggestion.ObservationIDs) == 0 {
			t.Fatalf("suggestion lost prompt or citations: %+v", suggestion)
		}
		for _, observation := range suggestion.ObservationIDs {
			if observation != left && observation != right {
				t.Fatalf("suggestion cited an unrelated observation: %+v", suggestion)
			}
		}
	}
}
