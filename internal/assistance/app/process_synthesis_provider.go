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
	pkgerrors "github.com/0xsj/overwatch-backend/pkg/errors"
	"github.com/0xsj/overwatch-backend/pkg/execx"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

const SynthesisInputPlaceholder = "{input}"

var (
	ErrSynthesisProviderUnavailable = pkgerrors.New(pkgerrors.Unavailable, "external synthesis provider is not configured")
	ErrSynthesisProviderTimedOut    = pkgerrors.New(pkgerrors.Timeout, "external synthesis provider timed out")
	ErrSynthesisProviderOutputLimit = pkgerrors.New(pkgerrors.Unprocessable, "external synthesis provider output limit exceeded")
)

type SynthesisProcessProviderConfig struct {
	Binary    string
	Args      []string
	Timeout   time.Duration
	MaxOutput int64
}

// ProcessSynthesisProvider adapts an optional model or gateway command. The
// process receives only the selected observations and returns a bounded,
// reviewable synthesis. It cannot access the workspace or any other source.
type ProcessSynthesisProvider struct {
	config SynthesisProcessProviderConfig
}

type synthesisProcessInput struct {
	Observations []synthesisProcessObservation `json:"observations"`
}

type synthesisProcessObservation struct {
	ObservationID string `json:"observation_id"`
	SourceTitle   string `json:"source_title"`
	Statement     string `json:"statement"`
	Quote         string `json:"quote"`
}

type synthesisProcessOutput struct {
	Text       string                      `json:"text"`
	Candidates []synthesisProcessCandidate `json:"candidates"`
}

type synthesisProcessCandidate struct {
	Kind           string   `json:"kind"`
	Name           string   `json:"name"`
	ObservationIDs []string `json:"observation_ids"`
	Rationale      string   `json:"rationale"`
}

func NewProcessSynthesisProvider(config SynthesisProcessProviderConfig) ProcessSynthesisProvider {
	if len(config.Args) == 0 {
		config.Args = []string{SynthesisInputPlaceholder}
	}
	if config.Timeout <= 0 {
		config.Timeout = 2 * time.Minute
	}
	if config.MaxOutput <= 0 {
		config.MaxOutput = 8 << 20
	}
	return ProcessSynthesisProvider{config: config}
}

func (p ProcessSynthesisProvider) Name() string   { return "external-process" }
func (p ProcessSynthesisProvider) Method() string { return "json-selected-observations-v1" }
func (p ProcessSynthesisProvider) External() bool { return true }

func (p ProcessSynthesisProvider) Synthesize(ctx context.Context, observations []Observation) (SynthesisOutput, error) {
	if p.config.Binary == "" {
		return SynthesisOutput{}, ErrSynthesisProviderUnavailable
	}
	if err := ctx.Err(); err != nil {
		return SynthesisOutput{}, err
	}
	input := synthesisProcessInput{Observations: make([]synthesisProcessObservation, 0, len(observations))}
	for _, observation := range observations {
		input.Observations = append(input.Observations, synthesisProcessObservation{
			ObservationID: observation.ID.String(), SourceTitle: observation.SourceTitle,
			Statement: observation.Statement, Quote: observation.Quote,
		})
	}
	payload, err := json.Marshal(input)
	if err != nil {
		return SynthesisOutput{}, fmt.Errorf("encode synthesis input: %w", err)
	}
	file, err := os.CreateTemp("", "overwatch-synthesis-")
	if err != nil {
		return SynthesisOutput{}, fmt.Errorf("prepare synthesis input: %w", err)
	}
	path := file.Name()
	defer os.Remove(path)
	if _, err := file.Write(payload); err != nil {
		_ = file.Close()
		return SynthesisOutput{}, fmt.Errorf("write synthesis input: %w", err)
	}
	if err := file.Close(); err != nil {
		return SynthesisOutput{}, fmt.Errorf("close synthesis input: %w", err)
	}

	args := append([]string(nil), p.config.Args...)
	foundPlaceholder := false
	for index, arg := range args {
		if arg == SynthesisInputPlaceholder {
			args[index] = path
			foundPlaceholder = true
		}
	}
	if !foundPlaceholder {
		args = append([]string{path}, args...)
	}
	result, spawnErr := execx.Spawn(ctx, append([]string{p.config.Binary}, args...), execx.Policy{Timeout: p.config.Timeout, MaxOutput: p.config.MaxOutput})
	if spawnErr != nil || result.Outcome == execx.Unavailable {
		if result.Reason != "" {
			return SynthesisOutput{}, fmt.Errorf("%w: %s", ErrSynthesisProviderUnavailable, result.Reason)
		}
		return SynthesisOutput{}, fmt.Errorf("%w: %v", ErrSynthesisProviderUnavailable, spawnErr)
	}
	if result.Outcome == execx.TimedOut {
		return SynthesisOutput{}, fmt.Errorf("%w: %s", ErrSynthesisProviderTimedOut, result.Reason)
	}
	if result.StdoutTruncated {
		return SynthesisOutput{}, fmt.Errorf("%w: %d bytes", ErrSynthesisProviderOutputLimit, p.config.MaxOutput)
	}
	if result.ExitCode != 0 {
		return SynthesisOutput{}, fmt.Errorf("synthesis provider exited with code %d", result.ExitCode)
	}
	var decoded synthesisProcessOutput
	decoder := json.NewDecoder(strings.NewReader(string(result.Stdout)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&decoded); err != nil {
		return SynthesisOutput{}, fmt.Errorf("synthesis provider returned invalid JSON: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return SynthesisOutput{}, stderrors.New("synthesis provider returned more than one JSON value")
		}
		return SynthesisOutput{}, fmt.Errorf("synthesis provider returned trailing data: %w", err)
	}
	if strings.TrimSpace(decoded.Text) == "" {
		return SynthesisOutput{}, stderrors.New("synthesis provider returned empty text")
	}

	known := make(map[id.ID]Observation, len(observations))
	for _, observation := range observations {
		known[observation.ID] = observation
	}
	candidates := make([]domain.SynthesisCandidate, 0, len(decoded.Candidates))
	for _, candidate := range decoded.Candidates {
		parsedIDs := make([]id.ID, 0, len(candidate.ObservationIDs))
		for _, rawID := range candidate.ObservationIDs {
			parsed, err := id.Parse(rawID)
			if err != nil {
				return SynthesisOutput{}, fmt.Errorf("synthesis provider returned an invalid observation id: %w", err)
			}
			observation, selected := known[parsed]
			if !selected {
				return SynthesisOutput{}, fmt.Errorf("synthesis provider cited an unselected observation %s", rawID)
			}
			if !strings.Contains(observation.Statement+"\n"+observation.Quote, candidate.Name) {
				return SynthesisOutput{}, fmt.Errorf("synthesis provider candidate %q is not exact in observation %s", candidate.Name, rawID)
			}
			parsedIDs = append(parsedIDs, parsed)
		}
		candidates = append(candidates, domain.SynthesisCandidate{Kind: candidate.Kind, Name: candidate.Name, ObservationIDs: parsedIDs, Rationale: candidate.Rationale})
	}
	return SynthesisOutput{Text: decoded.Text, Candidates: candidates}, nil
}

var _ SynthesisProvider = ProcessSynthesisProvider{}
