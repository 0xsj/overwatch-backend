package domain

import (
	"testing"
	"time"

	"github.com/0xsj/overwatch-backend/pkg/id"
)

func TestNewQuestionSuggestionsResultRetainsProviderFailureWithoutOutput(t *testing.T) {
	gap := QuestionSuggestionGap{Kind: QuestionGapCorroboration, Label: "the account", Detail: "independent support is missing", ObservationIDs: []id.ID{{4}}}
	found, err := NewQuestionSuggestionsResult(id.ID{1}, id.ID{2}, id.ID{3}, []QuestionSuggestionGap{gap}, "external-process", "question-v1", "question-v1", QuestionSuggestionsUnsupported, "", nil, "provider is not configured", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if found.Status != QuestionSuggestionsUnsupported || found.Error == "" || found.Output != "" || len(found.Suggestions) != 0 {
		t.Fatalf("unexpected failed question suggestions: %+v", found)
	}
	if _, err := NewQuestionSuggestionsResult(id.ID{1}, id.ID{2}, id.ID{3}, []QuestionSuggestionGap{gap}, "local", "question-v1", "question-v1", QuestionSuggestionsFailed, "output", nil, "provider failed", time.Now()); err == nil {
		t.Fatal("failed question suggestions should not retain output")
	}
}
