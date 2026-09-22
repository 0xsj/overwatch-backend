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

const BriefDraftInputPlaceholder = "{input}"

type BriefDraftProcessProviderConfig struct {
	Binary    string
	Args      []string
	Timeout   time.Duration
	MaxOutput int64
}

// ProcessBriefDraftProvider adapts an optional model or gateway command. The
// process receives an authored brief snapshot and selected observations and
// returns a reviewable diff; it cannot mutate the authored brief itself.
type ProcessBriefDraftProvider struct {
	config BriefDraftProcessProviderConfig
}

type briefDraftProcessInput struct {
	Brief        briefDraftProcessBrief         `json:"brief"`
	Observations []briefDraftProcessObservation `json:"observations"`
}

type briefDraftProcessBrief struct {
	BriefID        string   `json:"brief_id"`
	Title          string   `json:"title"`
	Question       string   `json:"question"`
	CurrentAccount string   `json:"current_account,omitempty"`
	Alternatives   string   `json:"alternatives,omitempty"`
	Limitations    string   `json:"limitations,omitempty"`
	NextSteps      string   `json:"next_steps,omitempty"`
	ObservationIDs []string `json:"observation_ids"`
}

type briefDraftProcessObservation struct {
	ObservationID string `json:"observation_id"`
	SourceTitle   string `json:"source_title"`
	Statement     string `json:"statement"`
	Quote         string `json:"quote"`
}

type briefDraftProcessOutput struct {
	Status  string                    `json:"status"`
	Output  string                    `json:"output"`
	Changes []briefDraftProcessChange `json:"changes"`
}

type briefDraftProcessChange struct {
	Section        string   `json:"section"`
	Before         string   `json:"before"`
	After          string   `json:"after"`
	Rationale      string   `json:"rationale"`
	ObservationIDs []string `json:"observation_ids"`
}

func NewProcessBriefDraftProvider(config BriefDraftProcessProviderConfig) ProcessBriefDraftProvider {
	if len(config.Args) == 0 {
		config.Args = []string{BriefDraftInputPlaceholder}
	}
	if config.Timeout <= 0 {
		config.Timeout = 2 * time.Minute
	}
	if config.MaxOutput <= 0 {
		config.MaxOutput = 8 << 20
	}
	return ProcessBriefDraftProvider{config: config}
}

func (p ProcessBriefDraftProvider) Name() string            { return "external-process" }
func (p ProcessBriefDraftProvider) Method() string          { return "json-working-brief-diff-v1" }
func (p ProcessBriefDraftProvider) TemplateVersion() string { return "brief-draft-v1" }
func (p ProcessBriefDraftProvider) External() bool          { return true }

func (p ProcessBriefDraftProvider) DraftBrief(ctx context.Context, input BriefDraftInput) (domain.BriefDraftStatus, string, []domain.BriefDraftChange, error) {
	if p.config.Binary == "" {
		return "", "", nil, ErrProcessProviderUnavailable
	}
	if err := ctx.Err(); err != nil {
		return "", "", nil, err
	}
	encoded := briefDraftProcessInput{
		Brief: briefDraftProcessBrief{
			BriefID: input.Brief.BriefID.String(), Title: input.Brief.Title, Question: input.Brief.Question,
			CurrentAccount: input.Brief.CurrentAccount, Alternatives: input.Brief.Alternatives,
			Limitations: input.Brief.Limitations, NextSteps: input.Brief.NextSteps,
			ObservationIDs: briefObservationStrings(input.Brief.ObservationIDs),
		},
		Observations: make([]briefDraftProcessObservation, 0, len(input.Observations)),
	}
	for _, observation := range input.Observations {
		encoded.Observations = append(encoded.Observations, briefDraftProcessObservation{
			ObservationID: observation.ID.String(), SourceTitle: observation.SourceTitle,
			Statement: observation.Statement, Quote: observation.Quote,
		})
	}
	payload, err := json.Marshal(encoded)
	if err != nil {
		return "", "", nil, fmt.Errorf("encode brief draft input: %w", err)
	}
	stdout, err := p.run(ctx, payload)
	if err != nil {
		return "", "", nil, err
	}
	var decoded briefDraftProcessOutput
	decoder := json.NewDecoder(strings.NewReader(string(stdout)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&decoded); err != nil {
		return "", "", nil, fmt.Errorf("brief draft provider returned invalid JSON: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return "", "", nil, stderrors.New("brief draft provider returned more than one JSON value")
		}
		return "", "", nil, fmt.Errorf("brief draft provider returned trailing data: %w", err)
	}
	status, err := domain.ParseBriefDraftStatus(decoded.Status)
	if err != nil {
		return "", "", nil, fmt.Errorf("brief draft provider returned invalid status: %w", err)
	}
	changes := make([]domain.BriefDraftChange, 0, len(decoded.Changes))
	for _, change := range decoded.Changes {
		section, err := domain.ParseBriefDraftSection(change.Section)
		if err != nil {
			return "", "", nil, fmt.Errorf("brief draft provider returned invalid section: %w", err)
		}
		citations := make([]id.ID, 0, len(change.ObservationIDs))
		for _, rawID := range change.ObservationIDs {
			parsed, err := id.Parse(rawID)
			if err != nil {
				return "", "", nil, fmt.Errorf("brief draft provider returned an invalid observation id: %w", err)
			}
			citations = append(citations, parsed)
		}
		changes = append(changes, domain.BriefDraftChange{Section: section, Before: change.Before, After: change.After, Rationale: change.Rationale, ObservationIDs: citations})
	}
	return status, decoded.Output, changes, nil
}

func briefObservationStrings(values []id.ID) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		out = append(out, value.String())
	}
	return out
}

func (p ProcessBriefDraftProvider) run(ctx context.Context, payload []byte) ([]byte, error) {
	file, err := os.CreateTemp("", "overwatch-brief-draft-")
	if err != nil {
		return nil, fmt.Errorf("prepare brief draft input: %w", err)
	}
	path := file.Name()
	defer os.Remove(path)
	if _, err := file.Write(payload); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("write brief draft input: %w", err)
	}
	if err := file.Close(); err != nil {
		return nil, fmt.Errorf("close brief draft input: %w", err)
	}
	args := append([]string(nil), p.config.Args...)
	found := false
	for index, arg := range args {
		if arg == BriefDraftInputPlaceholder {
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
		return nil, fmt.Errorf("brief draft provider exited with code %d", result.ExitCode)
	}
	return result.Stdout, nil
}

var _ BriefDraftProvider = ProcessBriefDraftProvider{}
