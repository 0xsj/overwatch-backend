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

const ConnectionReviewInputPlaceholder = "{input}"

type ConnectionReviewProcessProviderConfig struct {
	Binary    string
	Args      []string
	Timeout   time.Duration
	MaxOutput int64
}

// ProcessConnectionReviewProvider adapts an optional model or gateway command.
// The process receives one authored connection and its selected evidence, then
// returns review findings without mutating the relationship itself.
type ProcessConnectionReviewProvider struct {
	config ConnectionReviewProcessProviderConfig
}

type connectionReviewProcessInput struct {
	Connection             connectionReviewProcessConnection    `json:"connection"`
	SupportingObservations []connectionReviewProcessObservation `json:"supporting_observations"`
	OpposingObservations   []connectionReviewProcessObservation `json:"opposing_observations"`
	Decisions              []connectionReviewProcessDecision    `json:"decisions"`
}

type connectionReviewProcessConnection struct {
	ConnectionID             string   `json:"connection_id"`
	FromRecordID             string   `json:"from_record_id"`
	ToRecordID               string   `json:"to_record_id"`
	Kind                     string   `json:"kind"`
	State                    string   `json:"state"`
	Rationale                string   `json:"rationale"`
	SupportingObservationIDs []string `json:"supporting_observation_ids"`
	OpposingObservationIDs   []string `json:"opposing_observation_ids"`
}

type connectionReviewProcessObservation struct {
	ObservationID string `json:"observation_id"`
	SourceTitle   string `json:"source_title"`
	Statement     string `json:"statement"`
	Quote         string `json:"quote"`
}

type connectionReviewProcessDecision struct {
	LeftObservationID  string `json:"left_observation_id"`
	RightObservationID string `json:"right_observation_id"`
	Kind               string `json:"kind"`
	Rationale          string `json:"rationale"`
}

type connectionReviewProcessOutput struct {
	Status   string                           `json:"status"`
	Text     string                           `json:"text"`
	Findings []connectionReviewProcessFinding `json:"findings"`
}

type connectionReviewProcessFinding struct {
	Kind           string   `json:"kind"`
	Summary        string   `json:"summary"`
	ObservationIDs []string `json:"observation_ids"`
}

func NewProcessConnectionReviewProvider(config ConnectionReviewProcessProviderConfig) ProcessConnectionReviewProvider {
	if len(config.Args) == 0 {
		config.Args = []string{ConnectionReviewInputPlaceholder}
	}
	if config.Timeout <= 0 {
		config.Timeout = 2 * time.Minute
	}
	if config.MaxOutput <= 0 {
		config.MaxOutput = 8 << 20
	}
	return ProcessConnectionReviewProvider{config: config}
}

func (p ProcessConnectionReviewProvider) Name() string            { return "external-process" }
func (p ProcessConnectionReviewProvider) Method() string          { return "json-authored-connection-review-v1" }
func (p ProcessConnectionReviewProvider) TemplateVersion() string { return "connection-review-v1" }
func (p ProcessConnectionReviewProvider) External() bool          { return true }

func (p ProcessConnectionReviewProvider) ReviewConnection(ctx context.Context, input ConnectionReviewInput) (ConnectionReviewOutput, error) {
	if p.config.Binary == "" {
		return ConnectionReviewOutput{}, ErrProcessProviderUnavailable
	}
	if err := ctx.Err(); err != nil {
		return ConnectionReviewOutput{}, err
	}
	encoded := connectionReviewProcessInput{
		Connection: connectionReviewProcessConnection{
			ConnectionID: input.ConnectionID.String(), FromRecordID: input.FromRecordID.String(), ToRecordID: input.ToRecordID.String(),
			Kind: input.ConnectionKind, State: input.ConnectionState, Rationale: input.ConnectionRationale,
			SupportingObservationIDs: connectionObservationStrings(input.SupportingObservationIDs),
			OpposingObservationIDs:   connectionObservationStrings(input.OpposingObservationIDs),
		},
		SupportingObservations: connectionProcessObservations(input.SupportingObservations),
		OpposingObservations:   connectionProcessObservations(input.OpposingObservations),
		Decisions:              make([]connectionReviewProcessDecision, 0, len(input.Decisions)),
	}
	for _, decision := range input.Decisions {
		encoded.Decisions = append(encoded.Decisions, connectionReviewProcessDecision{
			LeftObservationID: decision.LeftObservationID.String(), RightObservationID: decision.RightObservationID.String(),
			Kind: decision.Kind, Rationale: decision.Rationale,
		})
	}
	payload, err := json.Marshal(encoded)
	if err != nil {
		return ConnectionReviewOutput{}, fmt.Errorf("encode connection review input: %w", err)
	}
	stdout, err := p.run(ctx, payload)
	if err != nil {
		return ConnectionReviewOutput{}, err
	}
	var decoded connectionReviewProcessOutput
	decoder := json.NewDecoder(strings.NewReader(string(stdout)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&decoded); err != nil {
		return ConnectionReviewOutput{}, fmt.Errorf("connection review provider returned invalid JSON: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return ConnectionReviewOutput{}, stderrors.New("connection review provider returned more than one JSON value")
		}
		return ConnectionReviewOutput{}, fmt.Errorf("connection review provider returned trailing data: %w", err)
	}
	status, err := domain.ParseConnectionReviewStatus(decoded.Status)
	if err != nil {
		return ConnectionReviewOutput{}, fmt.Errorf("connection review provider returned invalid status: %w", err)
	}
	findings := make([]domain.ConnectionReviewFinding, 0, len(decoded.Findings))
	for _, finding := range decoded.Findings {
		kind, err := domain.ParseConnectionReviewFindingKind(finding.Kind)
		if err != nil {
			return ConnectionReviewOutput{}, fmt.Errorf("connection review provider returned invalid finding kind: %w", err)
		}
		citations := make([]id.ID, 0, len(finding.ObservationIDs))
		for _, rawID := range finding.ObservationIDs {
			parsed, err := id.Parse(rawID)
			if err != nil {
				return ConnectionReviewOutput{}, fmt.Errorf("connection review provider returned an invalid observation id: %w", err)
			}
			citations = append(citations, parsed)
		}
		findings = append(findings, domain.ConnectionReviewFinding{Kind: kind, Summary: finding.Summary, ObservationIDs: citations})
	}
	return ConnectionReviewOutput{Status: status, Text: decoded.Text, Findings: findings}, nil
}

func connectionObservationStrings(values []id.ID) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		out = append(out, value.String())
	}
	return out
}

func connectionProcessObservations(values []Observation) []connectionReviewProcessObservation {
	out := make([]connectionReviewProcessObservation, 0, len(values))
	for _, observation := range values {
		out = append(out, connectionReviewProcessObservation{
			ObservationID: observation.ID.String(), SourceTitle: observation.SourceTitle,
			Statement: observation.Statement, Quote: observation.Quote,
		})
	}
	return out
}

func (p ProcessConnectionReviewProvider) run(ctx context.Context, payload []byte) ([]byte, error) {
	file, err := os.CreateTemp("", "overwatch-connection-review-")
	if err != nil {
		return nil, fmt.Errorf("prepare connection review input: %w", err)
	}
	path := file.Name()
	defer os.Remove(path)
	if _, err := file.Write(payload); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("write connection review input: %w", err)
	}
	if err := file.Close(); err != nil {
		return nil, fmt.Errorf("close connection review input: %w", err)
	}
	args := append([]string(nil), p.config.Args...)
	found := false
	for index, arg := range args {
		if arg == ConnectionReviewInputPlaceholder {
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
		return nil, fmt.Errorf("connection review provider exited with code %d", result.ExitCode)
	}
	return result.Stdout, nil
}

var _ ConnectionReviewProvider = ProcessConnectionReviewProvider{}
