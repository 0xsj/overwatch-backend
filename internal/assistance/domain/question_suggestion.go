package domain

import (
	"strings"
	"time"
	"unicode/utf8"

	"github.com/0xsj/overwatch-backend/pkg/errors"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

const (
	MaxQuestionSuggestionGaps            = 8
	MaxQuestionSuggestionGapObservations = 12
	MaxQuestionSuggestionObservations    = 24
	MaxQuestionSuggestions               = 12
	MaxQuestionSuggestionOutput          = 24000
	MaxQuestionSuggestionPrompt          = 400
	MaxQuestionSuggestionContext         = 4000
	MaxQuestionSuggestionGapLabel        = 400
	MaxQuestionSuggestionGapDetail       = 4000
)

const EventQuestionSuggestionsGenerated = "assistance.question_suggestions.generated"

type QuestionSuggestionStatus string

const (
	QuestionSuggestionsCompleted QuestionSuggestionStatus = "completed"
	QuestionSuggestionsEmpty     QuestionSuggestionStatus = "empty"
)

func (s QuestionSuggestionStatus) String() string { return string(s) }

func ParseQuestionSuggestionStatus(raw string) (QuestionSuggestionStatus, error) {
	switch QuestionSuggestionStatus(strings.TrimSpace(raw)) {
	case QuestionSuggestionsCompleted, QuestionSuggestionsEmpty:
		return QuestionSuggestionStatus(strings.TrimSpace(raw)), nil
	default:
		return "", errors.New(errors.Invalid, "question suggestion status must be completed or empty")
	}
}

type QuestionGapKind string

const (
	QuestionGapUnresolvedRelation QuestionGapKind = "unresolved_relation"
	QuestionGapContradiction      QuestionGapKind = "contradiction"
	QuestionGapCorroboration      QuestionGapKind = "corroboration_gap"
	QuestionGapReviewIncomplete   QuestionGapKind = "review_incomplete"
	QuestionGapOpenQuestion       QuestionGapKind = "open_question"
)

func (k QuestionGapKind) String() string { return string(k) }

func ParseQuestionGapKind(raw string) (QuestionGapKind, error) {
	switch QuestionGapKind(strings.TrimSpace(raw)) {
	case QuestionGapUnresolvedRelation, QuestionGapContradiction, QuestionGapCorroboration, QuestionGapReviewIncomplete, QuestionGapOpenQuestion:
		return QuestionGapKind(strings.TrimSpace(raw)), nil
	default:
		return "", errors.New(errors.Invalid, "question suggestion gap kind is unknown")
	}
}

type QuestionSuggestionGap struct {
	Kind           QuestionGapKind `json:"kind"`
	Label          string          `json:"label"`
	Detail         string          `json:"detail"`
	ObservationIDs []id.ID         `json:"observation_ids"`
}

type QuestionSuggestion struct {
	Kind           QuestionGapKind `json:"kind"`
	Prompt         string          `json:"prompt"`
	Context        string          `json:"context"`
	ObservationIDs []id.ID         `json:"observation_ids"`
}

// QuestionSuggestions is a persisted assistant proposal. It snapshots the
// unresolved gap inputs and keeps suggested questions separate from authored
// lead questions until a person chooses to save one.
type QuestionSuggestions struct {
	ID              id.ID                    `json:"question_suggestions_id"`
	WorkspaceID     id.ID                    `json:"workspace_id"`
	Gaps            []QuestionSuggestionGap  `json:"gaps"`
	Provider        string                   `json:"provider"`
	Method          string                   `json:"method"`
	TemplateVersion string                   `json:"template_version"`
	Status          QuestionSuggestionStatus `json:"status"`
	Output          string                   `json:"output"`
	Suggestions     []QuestionSuggestion     `json:"suggestions"`
	CreatedBy       id.ID                    `json:"created_by"`
	CreatedAt       time.Time                `json:"created_at"`
}

func NewQuestionSuggestions(want, workspace, actor id.ID, gaps []QuestionSuggestionGap, provider, method, templateVersion string, status QuestionSuggestionStatus, output string, suggestions []QuestionSuggestion, at time.Time) (QuestionSuggestions, error) {
	if want.IsZero() || workspace.IsZero() || actor.IsZero() {
		return QuestionSuggestions{}, ErrIDRequired
	}
	cleanGaps, selected, err := cleanQuestionSuggestionGaps(gaps)
	if err != nil {
		return QuestionSuggestions{}, err
	}
	if len(cleanGaps) == 0 {
		return QuestionSuggestions{}, ErrQuestionSuggestionRequired
	}
	if len([]rune(strings.TrimSpace(provider))) == 0 || len(provider) > 200 || !utf8.ValidString(provider) {
		return QuestionSuggestions{}, ErrProviderRequired
	}
	if len([]rune(strings.TrimSpace(method))) == 0 || len(method) > 200 || !utf8.ValidString(method) {
		return QuestionSuggestions{}, ErrMethodRequired
	}
	if len([]rune(strings.TrimSpace(templateVersion))) == 0 || len(templateVersion) > 200 || !utf8.ValidString(templateVersion) {
		return QuestionSuggestions{}, ErrMethodRequired
	}
	parsedStatus, err := ParseQuestionSuggestionStatus(status.String())
	if err != nil {
		return QuestionSuggestions{}, err
	}
	if !utf8.ValidString(output) || strings.ContainsRune(output, 0) || len(output) > MaxQuestionSuggestionOutput {
		return QuestionSuggestions{}, ErrQuestionSuggestionOutput
	}
	if len(suggestions) > MaxQuestionSuggestions {
		return QuestionSuggestions{}, ErrQuestionSuggestionOutput
	}
	cleanSuggestions := make([]QuestionSuggestion, 0, len(suggestions))
	seenPrompts := make(map[string]struct{}, len(suggestions))
	for _, suggestion := range suggestions {
		kind, err := ParseQuestionGapKind(suggestion.Kind.String())
		if err != nil {
			return QuestionSuggestions{}, err
		}
		prompt := strings.TrimSpace(suggestion.Prompt)
		contextText := strings.TrimSpace(suggestion.Context)
		if prompt == "" || len(prompt) > MaxQuestionSuggestionPrompt || !utf8.ValidString(prompt) || strings.ContainsRune(prompt, 0) {
			return QuestionSuggestions{}, ErrQuestionSuggestionOutput
		}
		if !utf8.ValidString(contextText) || strings.ContainsRune(contextText, 0) || len(contextText) > MaxQuestionSuggestionContext {
			return QuestionSuggestions{}, ErrQuestionSuggestionOutput
		}
		if _, exists := seenPrompts[prompt]; exists {
			return QuestionSuggestions{}, ErrCandidateInvalid
		}
		seenPrompts[prompt] = struct{}{}
		citations, err := questionSuggestionIDs(suggestion.ObservationIDs)
		if err != nil {
			return QuestionSuggestions{}, err
		}
		if len(citations) == 0 {
			return QuestionSuggestions{}, ErrCandidateInvalid
		}
		for _, observation := range citations {
			if _, exists := selected[observation]; !exists {
				return QuestionSuggestions{}, ErrCandidateInvalid
			}
		}
		cleanSuggestions = append(cleanSuggestions, QuestionSuggestion{Kind: kind, Prompt: prompt, Context: contextText, ObservationIDs: citations})
	}
	if at.IsZero() {
		return QuestionSuggestions{}, ErrTimeRequired
	}
	return QuestionSuggestions{ID: want, WorkspaceID: workspace, Gaps: cleanGaps, Provider: strings.TrimSpace(provider), Method: strings.TrimSpace(method), TemplateVersion: strings.TrimSpace(templateVersion), Status: parsedStatus, Output: output, Suggestions: cleanSuggestions, CreatedBy: actor, CreatedAt: at}, nil
}

func cleanQuestionSuggestionGaps(input []QuestionSuggestionGap) ([]QuestionSuggestionGap, map[id.ID]struct{}, error) {
	if len(input) > MaxQuestionSuggestionGaps {
		return nil, nil, ErrQuestionSuggestionTooLarge
	}
	selected := make(map[id.ID]struct{})
	clean := make([]QuestionSuggestionGap, 0, len(input))
	for _, gap := range input {
		kind, err := ParseQuestionGapKind(gap.Kind.String())
		if err != nil {
			return nil, nil, err
		}
		label := strings.TrimSpace(gap.Label)
		detail := strings.TrimSpace(gap.Detail)
		if label == "" || len(label) > MaxQuestionSuggestionGapLabel || !utf8.ValidString(label) || strings.ContainsRune(label, 0) {
			return nil, nil, ErrQuestionSuggestionGap
		}
		if !utf8.ValidString(detail) || strings.ContainsRune(detail, 0) || len(detail) > MaxQuestionSuggestionGapDetail {
			return nil, nil, ErrQuestionSuggestionGap
		}
		ids, err := questionSuggestionIDs(gap.ObservationIDs)
		if err != nil {
			return nil, nil, err
		}
		if len(ids) == 0 {
			return nil, nil, ErrQuestionSuggestionGap
		}
		for _, observation := range ids {
			selected[observation] = struct{}{}
		}
		clean = append(clean, QuestionSuggestionGap{Kind: kind, Label: label, Detail: detail, ObservationIDs: ids})
	}
	if len(selected) > MaxQuestionSuggestionObservations {
		return nil, nil, ErrQuestionSuggestionTooLarge
	}
	return clean, selected, nil
}

func questionSuggestionIDs(input []id.ID) ([]id.ID, error) {
	if len(input) > MaxQuestionSuggestionGapObservations {
		return nil, ErrQuestionSuggestionTooLarge
	}
	seen := make(map[id.ID]struct{}, len(input))
	out := make([]id.ID, 0, len(input))
	for _, observation := range input {
		if observation.IsZero() {
			return nil, ErrIDRequired
		}
		if _, exists := seen[observation]; exists {
			return nil, ErrCandidateInvalid
		}
		seen[observation] = struct{}{}
		out = append(out, observation)
	}
	return out, nil
}
