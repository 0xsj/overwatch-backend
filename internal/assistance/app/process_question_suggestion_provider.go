package app

import (
	"context"
	"encoding/json"
	stderrors "errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/0xsj/overwatch-backend/internal/assistance/domain"
	"github.com/0xsj/overwatch-backend/pkg/execx"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

const QuestionSuggestionsInputPlaceholder = "{input}"

type QuestionSuggestionsProcessProviderConfig struct {
	Binary    string
	Args      []string
	Timeout   time.Duration
	MaxOutput int64
}

// ProcessQuestionSuggestionProvider adapts an optional model or gateway
// command. The process receives only the selected unresolved gaps and their
// observations; returned prompts remain proposals until a researcher saves one.
type ProcessQuestionSuggestionProvider struct {
	config QuestionSuggestionsProcessProviderConfig
}

type questionSuggestionsProcessInput struct {
	Gaps []questionSuggestionsProcessGap `json:"gaps"`
}

type questionSuggestionsProcessGap struct {
	Kind           string                                  `json:"kind"`
	Label          string                                  `json:"label"`
	Detail         string                                  `json:"detail"`
	ObservationIDs []string                                `json:"observation_ids"`
	Observations   []questionSuggestionsProcessObservation `json:"observations"`
}

type questionSuggestionsProcessObservation struct {
	ObservationID string `json:"observation_id"`
	SourceTitle   string `json:"source_title"`
	Statement     string `json:"statement"`
	Quote         string `json:"quote"`
}

type questionSuggestionsProcessOutput struct {
	Status      string                                 `json:"status"`
	Text        string                                 `json:"text"`
	Suggestions []questionSuggestionsProcessSuggestion `json:"suggestions"`
}

type questionSuggestionsProcessSuggestion struct {
	Kind           string   `json:"kind"`
	Prompt         string   `json:"prompt"`
	Context        string   `json:"context"`
	ObservationIDs []string `json:"observation_ids"`
}

func NewProcessQuestionSuggestionProvider(config QuestionSuggestionsProcessProviderConfig) ProcessQuestionSuggestionProvider {
	if len(config.Args) == 0 {
		config.Args = []string{QuestionSuggestionsInputPlaceholder}
	}
	if config.Timeout <= 0 {
		config.Timeout = 2 * time.Minute
	}
	if config.MaxOutput <= 0 {
		config.MaxOutput = 8 << 20
	}
	return ProcessQuestionSuggestionProvider{config: config}
}

func (p ProcessQuestionSuggestionProvider) Name() string            { return "external-process" }
func (p ProcessQuestionSuggestionProvider) Method() string          { return "json-evidence-gap-questions-v1" }
func (p ProcessQuestionSuggestionProvider) TemplateVersion() string { return "question-suggestions-v1" }
func (p ProcessQuestionSuggestionProvider) External() bool          { return true }

func (p ProcessQuestionSuggestionProvider) SuggestQuestions(ctx context.Context, input QuestionSuggestionInput) (QuestionSuggestionOutput, error) {
	if p.config.Binary == "" {
		return QuestionSuggestionOutput{}, ErrProcessProviderUnavailable
	}
	if err := ctx.Err(); err != nil {
		return QuestionSuggestionOutput{}, err
	}
	encoded := questionSuggestionsProcessInput{Gaps: make([]questionSuggestionsProcessGap, 0, len(input.Gaps))}
	for _, gap := range input.Gaps {
		encodedGap := questionSuggestionsProcessGap{
			Kind: gap.Kind.String(), Label: gap.Label, Detail: gap.Detail,
			ObservationIDs: questionObservationStrings(gap.ObservationIDs),
			Observations:   make([]questionSuggestionsProcessObservation, 0, len(gap.Observations)),
		}
		for _, observation := range gap.Observations {
			encodedGap.Observations = append(encodedGap.Observations, questionSuggestionsProcessObservation{
				ObservationID: observation.ID.String(), SourceTitle: observation.SourceTitle,
				Statement: observation.Statement, Quote: observation.Quote,
			})
		}
		encoded.Gaps = append(encoded.Gaps, encodedGap)
	}
	payload, err := json.Marshal(encoded)
	if err != nil {
		return QuestionSuggestionOutput{}, fmt.Errorf("encode question suggestion input: %w", err)
	}
	stdout, err := p.run(ctx, payload)
	if err != nil {
		return QuestionSuggestionOutput{}, err
	}
	var decoded questionSuggestionsProcessOutput
	decoder := json.NewDecoder(strings.NewReader(string(stdout)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&decoded); err != nil {
		return QuestionSuggestionOutput{}, fmt.Errorf("question suggestion provider returned invalid JSON: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return QuestionSuggestionOutput{}, stderrors.New("question suggestion provider returned more than one JSON value")
		}
		return QuestionSuggestionOutput{}, fmt.Errorf("question suggestion provider returned trailing data: %w", err)
	}
	status, err := domain.ParseQuestionSuggestionStatus(decoded.Status)
	if err != nil {
		return QuestionSuggestionOutput{}, fmt.Errorf("question suggestion provider returned invalid status: %w", err)
	}
	suggestions := make([]domain.QuestionSuggestion, 0, len(decoded.Suggestions))
	for _, suggestion := range decoded.Suggestions {
		kind, err := domain.ParseQuestionGapKind(suggestion.Kind)
		if err != nil {
			return QuestionSuggestionOutput{}, fmt.Errorf("question suggestion provider returned invalid kind: %w", err)
		}
		citations := make([]id.ID, 0, len(suggestion.ObservationIDs))
		for _, rawID := range suggestion.ObservationIDs {
			parsed, err := id.Parse(rawID)
			if err != nil {
				return QuestionSuggestionOutput{}, fmt.Errorf("question suggestion provider returned an invalid observation id: %w", err)
			}
			citations = append(citations, parsed)
		}
		suggestions = append(suggestions, domain.QuestionSuggestion{Kind: kind, Prompt: suggestion.Prompt, Context: suggestion.Context, ObservationIDs: citations})
	}
	return QuestionSuggestionOutput{Status: status, Text: decoded.Text, Suggestions: suggestions}, nil
}

func questionObservationStrings(values []id.ID) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		out = append(out, value.String())
	}
	return out
}

func (p ProcessQuestionSuggestionProvider) run(ctx context.Context, payload []byte) ([]byte, error) {
	file, err := os.CreateTemp("", "overwatch-question-suggestions-")
	if err != nil {
		return nil, fmt.Errorf("prepare question suggestion input: %w", err)
	}
	path := file.Name()
	defer os.Remove(path)
	if _, err := file.Write(payload); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("write question suggestion input: %w", err)
	}
	if err := file.Close(); err != nil {
		return nil, fmt.Errorf("close question suggestion input: %w", err)
	}
	args := append([]string(nil), p.config.Args...)
	found := false
	for index, arg := range args {
		if arg == QuestionSuggestionsInputPlaceholder {
			args[index] = path
			found = true
		}
	}
	if !found {
		args = append([]string{path}, args...)
	}
	result, spawnErr := execx.Spawn(ctx, append([]string{p.config.Binary}, args...), execx.Policy{Timeout: p.config.Timeout, MaxOutput: p.config.MaxOutput})
	if spawnErr != nil || result.Outcome == execx.Unavailable {
		if result.Reason != "" {
			return nil, fmt.Errorf("%w: %s", ErrProcessProviderUnavailable, result.Reason)
		}
		return nil, fmt.Errorf("%w: %v", ErrProcessProviderUnavailable, spawnErr)
	}
	if result.Outcome == execx.TimedOut {
		return nil, fmt.Errorf("%w: %s", ErrProcessProviderTimedOut, result.Reason)
	}
	if result.StdoutTruncated {
		return nil, fmt.Errorf("%w: %d bytes", ErrProcessProviderOutputLimit, p.config.MaxOutput)
	}
	if result.ExitCode != 0 {
		return nil, fmt.Errorf("question suggestion provider exited with code %d", result.ExitCode)
	}
	return result.Stdout, nil
}

var _ QuestionSuggestionProvider = ProcessQuestionSuggestionProvider{}
