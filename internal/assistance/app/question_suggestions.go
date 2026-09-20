package app

import (
	"context"
	"fmt"
	"strings"

	"github.com/0xsj/overwatch-backend/internal/assistance/domain"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

type QuestionSuggestionGapInput struct {
	Kind           domain.QuestionGapKind
	Label          string
	Detail         string
	ObservationIDs []id.ID
	Observations   []Observation
}

type QuestionSuggestionInput struct {
	Gaps []QuestionSuggestionGapInput
}

type QuestionSuggestionOutput struct {
	Status      domain.QuestionSuggestionStatus
	Text        string
	Suggestions []domain.QuestionSuggestion
}

type QuestionSuggestionProvider interface {
	SuggestQuestions(context.Context, QuestionSuggestionInput) (QuestionSuggestionOutput, error)
	Name() string
	Method() string
	TemplateVersion() string
}

// LocalQuestionSuggestionProvider produces bounded prompts from the shape of
// an authored gap. It never creates a question or treats a gap as proof of a
// disputed fact.
type LocalQuestionSuggestionProvider struct{}

func (LocalQuestionSuggestionProvider) Name() string            { return "local" }
func (LocalQuestionSuggestionProvider) Method() string          { return "unresolved-evidence-gaps-v1" }
func (LocalQuestionSuggestionProvider) TemplateVersion() string { return "question-suggestions-v1" }

func (LocalQuestionSuggestionProvider) SuggestQuestions(ctx context.Context, input QuestionSuggestionInput) (QuestionSuggestionOutput, error) {
	if err := ctx.Err(); err != nil {
		return QuestionSuggestionOutput{}, err
	}
	suggestions := make([]domain.QuestionSuggestion, 0, len(input.Gaps))
	lines := []string{"Assisted next-question proposal", "", "The following prompts are grounded in selected unresolved evidence gaps. They remain drafts until an analyst saves one as a question.", ""}
	for _, gap := range input.Gaps {
		prompt, contextText := localQuestionSuggestion(gap)
		suggestions = append(suggestions, domain.QuestionSuggestion{Kind: gap.Kind, Prompt: prompt, Context: contextText, ObservationIDs: append([]id.ID(nil), gap.ObservationIDs...)})
		lines = append(lines, "- "+prompt)
	}
	status := domain.QuestionSuggestionsCompleted
	if len(suggestions) == 0 {
		status = domain.QuestionSuggestionsEmpty
	}
	return QuestionSuggestionOutput{Status: status, Text: strings.Join(lines, "\n"), Suggestions: suggestions}, nil
}

func localQuestionSuggestion(gap QuestionSuggestionGapInput) (string, string) {
	label := strings.TrimSpace(gap.Label)
	detail := strings.TrimSpace(gap.Detail)
	switch gap.Kind {
	case domain.QuestionGapContradiction:
		return fmt.Sprintf("What independent evidence would resolve the conflicting accounts about %s?", label), withGapDetail("The selected evidence is in tension. Identify a source or observation that can distinguish the competing accounts.", detail)
	case domain.QuestionGapCorroboration:
		return fmt.Sprintf("What independent source or observation would corroborate the working account about %s?", label), withGapDetail("The selected material does not yet provide enough corroboration. Specify what an independent source would need to show.", detail)
	case domain.QuestionGapReviewIncomplete:
		return fmt.Sprintf("Which unresolved comparison about %s should be reviewed next, and what would settle it?", label), withGapDetail("Some cited observations have not been compared or are only partly reviewed. Narrow the next review to a discriminating detail.", detail)
	case domain.QuestionGapOpenQuestion:
		return fmt.Sprintf("What evidence would answer the open question: %s?", label), withGapDetail("This prompt keeps the existing investigation question explicit while identifying the evidence needed to move it forward.", detail)
	default:
		return fmt.Sprintf("What evidence would resolve the unresolved relationship around %s?", label), withGapDetail("The selected evidence contains an unresolved relationship. Look for a source that can distinguish the competing interpretations.", detail)
	}
}

func withGapDetail(prefix, detail string) string {
	if detail == "" {
		return prefix
	}
	return prefix + " Current gap context: " + detail
}
