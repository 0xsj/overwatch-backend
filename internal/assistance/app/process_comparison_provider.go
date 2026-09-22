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

const ComparisonInputPlaceholder = "{input}"

type ComparisonProcessProviderConfig struct {
	Binary    string
	Args      []string
	Timeout   time.Duration
	MaxOutput int64
}

// ProcessComparisonProvider adapts an optional model or gateway command. The
// process receives only the selected observations and saved pair decisions;
// its findings must cite those selected observations exactly.
type ProcessComparisonProvider struct {
	config ComparisonProcessProviderConfig
}

type comparisonProcessInput struct {
	Observations []comparisonProcessObservation `json:"observations"`
	Decisions    []comparisonProcessDecision    `json:"decisions"`
}

type comparisonProcessObservation struct {
	ObservationID string `json:"observation_id"`
	SourceTitle   string `json:"source_title"`
	Statement     string `json:"statement"`
	Quote         string `json:"quote"`
}

type comparisonProcessDecision struct {
	LeftObservationID  string `json:"left_observation_id"`
	RightObservationID string `json:"right_observation_id"`
	Kind               string `json:"kind"`
	Rationale          string `json:"rationale"`
}

type comparisonProcessOutput struct {
	Status   string                     `json:"status"`
	Text     string                     `json:"text"`
	Findings []comparisonProcessFinding `json:"findings"`
}

type comparisonProcessFinding struct {
	Kind           string   `json:"kind"`
	Summary        string   `json:"summary"`
	ObservationIDs []string `json:"observation_ids"`
}

func NewProcessComparisonProvider(config ComparisonProcessProviderConfig) ProcessComparisonProvider {
	if len(config.Args) == 0 {
		config.Args = []string{ComparisonInputPlaceholder}
	}
	if config.Timeout <= 0 {
		config.Timeout = 2 * time.Minute
	}
	if config.MaxOutput <= 0 {
		config.MaxOutput = 8 << 20
	}
	return ProcessComparisonProvider{config: config}
}

func (p ProcessComparisonProvider) Name() string            { return "external-process" }
func (p ProcessComparisonProvider) Method() string          { return "json-selected-observations-comparison-v1" }
func (p ProcessComparisonProvider) TemplateVersion() string { return "comparison-v1" }
func (p ProcessComparisonProvider) External() bool          { return true }

func (p ProcessComparisonProvider) Compare(ctx context.Context, input ComparisonInput) (ComparisonOutput, error) {
	if p.config.Binary == "" {
		return ComparisonOutput{}, ErrProcessProviderUnavailable
	}
	if err := ctx.Err(); err != nil {
		return ComparisonOutput{}, err
	}
	encoded := comparisonProcessInput{
		Observations: make([]comparisonProcessObservation, 0, len(input.Observations)),
		Decisions:    make([]comparisonProcessDecision, 0, len(input.Decisions)),
	}
	for _, observation := range input.Observations {
		encoded.Observations = append(encoded.Observations, comparisonProcessObservation{
			ObservationID: observation.ID.String(), SourceTitle: observation.SourceTitle,
			Statement: observation.Statement, Quote: observation.Quote,
		})
	}
	for _, decision := range input.Decisions {
		encoded.Decisions = append(encoded.Decisions, comparisonProcessDecision{
			LeftObservationID: decision.LeftObservationID.String(), RightObservationID: decision.RightObservationID.String(),
			Kind: decision.Kind, Rationale: decision.Rationale,
		})
	}
	payload, err := json.Marshal(encoded)
	if err != nil {
		return ComparisonOutput{}, fmt.Errorf("encode comparison input: %w", err)
	}
	stdout, err := p.run(ctx, payload)
	if err != nil {
		return ComparisonOutput{}, err
	}
	var decoded comparisonProcessOutput
	decoder := json.NewDecoder(strings.NewReader(string(stdout)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&decoded); err != nil {
		return ComparisonOutput{}, fmt.Errorf("comparison provider returned invalid JSON: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return ComparisonOutput{}, stderrors.New("comparison provider returned more than one JSON value")
		}
		return ComparisonOutput{}, fmt.Errorf("comparison provider returned trailing data: %w", err)
	}
	status, err := domain.ParseComparisonStatus(decoded.Status)
	if err != nil {
		return ComparisonOutput{}, fmt.Errorf("comparison provider returned invalid status: %w", err)
	}
	findings := make([]domain.ComparisonFinding, 0, len(decoded.Findings))
	for _, finding := range decoded.Findings {
		citations := make([]id.ID, 0, len(finding.ObservationIDs))
		for _, rawID := range finding.ObservationIDs {
			parsed, err := id.Parse(rawID)
			if err != nil {
				return ComparisonOutput{}, fmt.Errorf("comparison provider returned an invalid observation id: %w", err)
			}
			citations = append(citations, parsed)
		}
		findings = append(findings, domain.ComparisonFinding{Kind: domain.FindingKind(finding.Kind), Summary: finding.Summary, ObservationIDs: citations})
	}
	return ComparisonOutput{Status: status, Text: decoded.Text, Findings: findings}, nil
}

func (p ProcessComparisonProvider) run(ctx context.Context, payload []byte) ([]byte, error) {
	file, err := os.CreateTemp("", "overwatch-comparison-")
	if err != nil {
		return nil, fmt.Errorf("prepare comparison input: %w", err)
	}
	path := file.Name()
	defer os.Remove(path)
	if _, err := file.Write(payload); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("write comparison input: %w", err)
	}
	if err := file.Close(); err != nil {
		return nil, fmt.Errorf("close comparison input: %w", err)
	}
	args := append([]string(nil), p.config.Args...)
	found := false
	for index, arg := range args {
		if arg == ComparisonInputPlaceholder {
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
		return nil, fmt.Errorf("comparison provider exited with code %d", result.ExitCode)
	}
	return result.Stdout, nil
}

var _ ComparisonProvider = ProcessComparisonProvider{}
