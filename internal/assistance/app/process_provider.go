package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"
	"unicode/utf8"

	"github.com/0xsj/overwatch-backend/internal/assistance/domain"
	pkgerrors "github.com/0xsj/overwatch-backend/pkg/errors"
	"github.com/0xsj/overwatch-backend/pkg/execx"
)

const ProcessInputPlaceholder = "{input}"

var (
	ErrProcessProviderUnavailable = pkgerrors.New(pkgerrors.Unavailable, "external assistance provider is not configured")
	ErrProcessProviderTimedOut    = pkgerrors.New(pkgerrors.Timeout, "external assistance provider timed out")
	ErrProcessProviderOutputLimit = pkgerrors.New(pkgerrors.Unprocessable, "external assistance provider output limit exceeded")
)

type ProcessProviderConfig struct {
	Binary    string
	Args      []string
	Timeout   time.Duration
	MaxOutput int64
}

// ProcessProvider adapts a model or extraction command that accepts a JSON
// input file and returns exact, source-grounded passage proposals as JSON.
// It never invokes a shell and never inherits the server environment.
type ProcessProvider struct{ config ProcessProviderConfig }

type processInput struct {
	SourceID     string `json:"source_id"`
	CaptureID    string `json:"capture_id"`
	ExtractionID string `json:"extraction_id,omitempty"`
	Content      string `json:"content"`
}

type processDraft struct {
	Statement               string `json:"statement"`
	Quote                   string `json:"quote"`
	QuoteStart              int    `json:"quote_start"`
	QuoteEnd                int    `json:"quote_end,omitempty"`
	CandidateKind           string `json:"candidate_kind,omitempty"`
	CandidateName           string `json:"candidate_name,omitempty"`
	CandidateDescription    string `json:"candidate_description,omitempty"`
	RelationshipKind        string `json:"relationship_kind,omitempty"`
	RelatedCandidateKind    string `json:"related_candidate_kind,omitempty"`
	RelatedCandidateName    string `json:"related_candidate_name,omitempty"`
	RelationshipDescription string `json:"relationship_description,omitempty"`
}

func NewProcessProvider(config ProcessProviderConfig) ProcessProvider {
	if len(config.Args) == 0 {
		config.Args = []string{ProcessInputPlaceholder}
	}
	if config.Timeout <= 0 {
		config.Timeout = 2 * time.Minute
	}
	if config.MaxOutput <= 0 {
		config.MaxOutput = 8 << 20
	}
	return ProcessProvider{config: config}
}

func (p ProcessProvider) Name() string            { return "external-process" }
func (p ProcessProvider) Method() string          { return "json-passage-proposals-v1" }
func (p ProcessProvider) TemplateVersion() string { return "json-passage-proposals-v1" }
func (p ProcessProvider) External() bool          { return true }

func (p ProcessProvider) Extract(ctx context.Context, in Input) ([]domain.ProposalDraft, error) {
	if p.config.Binary == "" {
		return nil, ErrProcessProviderUnavailable
	}
	input := processInput{SourceID: in.SourceID.String(), CaptureID: in.CaptureID.String(), Content: in.Content}
	if !in.ExtractionID.IsZero() {
		input.ExtractionID = in.ExtractionID.String()
	}
	payload, err := json.Marshal(input)
	if err != nil {
		return nil, fmt.Errorf("encode assistance input: %w", err)
	}
	file, err := os.CreateTemp("", "overwatch-assistance-")
	if err != nil {
		return nil, fmt.Errorf("prepare assistance input: %w", err)
	}
	path := file.Name()
	defer os.Remove(path)
	if _, err := file.Write(payload); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("write assistance input: %w", err)
	}
	if err := file.Close(); err != nil {
		return nil, fmt.Errorf("close assistance input: %w", err)
	}

	args := append([]string(nil), p.config.Args...)
	found := false
	for index, arg := range args {
		if arg == ProcessInputPlaceholder {
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
		return nil, fmt.Errorf("assistance provider exited with code %d", result.ExitCode)
	}
	var drafts []processDraft
	if err := json.Unmarshal(result.Stdout, &drafts); err != nil {
		return nil, fmt.Errorf("assistance provider returned invalid JSON: %w", err)
	}
	if len(drafts) > domain.MaxProposals {
		drafts = drafts[:domain.MaxProposals]
	}
	out := make([]domain.ProposalDraft, 0, len(drafts))
	for _, draft := range drafts {
		end := draft.QuoteEnd
		if end == 0 {
			end = draft.QuoteStart + utf8.RuneCountInString(draft.Quote)
		}
		out = append(out, domain.ProposalDraft{Statement: draft.Statement, Quote: draft.Quote, QuoteStart: draft.QuoteStart, QuoteEnd: end, CandidateKind: draft.CandidateKind, CandidateName: draft.CandidateName, CandidateDescription: draft.CandidateDescription, RelationshipKind: draft.RelationshipKind, RelatedCandidateKind: draft.RelatedCandidateKind, RelatedCandidateName: draft.RelatedCandidateName, RelationshipDescription: draft.RelationshipDescription})
	}
	return out, nil
}

var _ Provider = ProcessProvider{}
