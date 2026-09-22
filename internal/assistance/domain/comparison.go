package domain

import (
	"strings"
	"time"
	"unicode/utf8"

	"github.com/0xsj/overwatch-backend/pkg/errors"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

const (
	MaxComparisonObservations = 6
	MaxComparisonFindings     = 50
	MaxComparisonOutput       = 24000
	MaxComparisonSummary      = 4000
	MaxComparisonError        = 2000
)

const EventComparisonGenerated = "assistance.comparison.generated"

type ComparisonStatus string

const (
	ComparisonCompleted   ComparisonStatus = "completed"
	ComparisonEmpty       ComparisonStatus = "empty"
	ComparisonFailed      ComparisonStatus = "failed"
	ComparisonUnsupported ComparisonStatus = "unsupported"
	ComparisonTimedOut    ComparisonStatus = "timed_out"
)

func (s ComparisonStatus) String() string { return string(s) }

func ParseComparisonStatus(raw string) (ComparisonStatus, error) {
	switch ComparisonStatus(strings.TrimSpace(raw)) {
	case ComparisonCompleted, ComparisonEmpty, ComparisonFailed, ComparisonUnsupported, ComparisonTimedOut:
		return ComparisonStatus(strings.TrimSpace(raw)), nil
	default:
		return "", errors.New(errors.Invalid, "comparison status is unknown")
	}
}

type FindingKind string

const (
	Agreement          FindingKind = "agreement"
	Contradiction      FindingKind = "contradiction"
	PossibleRepetition FindingKind = "possible_repetition"
	UniqueDetail       FindingKind = "unique_detail"
	CoverageGap        FindingKind = "coverage_gap"
)

func (k FindingKind) String() string { return string(k) }

func ParseFindingKind(raw string) (FindingKind, error) {
	switch FindingKind(strings.TrimSpace(raw)) {
	case Agreement, Contradiction, PossibleRepetition, UniqueDetail, CoverageGap:
		return FindingKind(strings.TrimSpace(raw)), nil
	default:
		return "", errors.New(errors.Invalid, "comparison finding kind is unknown")
	}
}

// ComparisonFinding is an assistant proposal anchored to exact selected
// observations. It is not a relation, a record, or a truth assessment.
type ComparisonFinding struct {
	Kind           FindingKind `json:"kind"`
	Summary        string      `json:"summary"`
	ObservationIDs []id.ID     `json:"observation_ids"`
}

type Comparison struct {
	ID              id.ID               `json:"comparison_id"`
	WorkspaceID     id.ID               `json:"workspace_id"`
	ObservationIDs  []id.ID             `json:"observation_ids"`
	Provider        string              `json:"provider"`
	Method          string              `json:"method"`
	TemplateVersion string              `json:"template_version"`
	Status          ComparisonStatus    `json:"status"`
	Output          string              `json:"output"`
	Findings        []ComparisonFinding `json:"findings"`
	CreatedBy       id.ID               `json:"created_by"`
	CreatedAt       time.Time           `json:"created_at"`
	Error           string              `json:"error,omitempty"`
}

func NewComparison(want, workspace, actor id.ID, observations []id.ID, provider, method, templateVersion string, status ComparisonStatus, output string, findings []ComparisonFinding, at time.Time) (Comparison, error) {
	return NewComparisonResult(want, workspace, actor, observations, provider, method, templateVersion, status, output, findings, "", at)
}

func NewComparisonResult(want, workspace, actor id.ID, observations []id.ID, provider, method, templateVersion string, status ComparisonStatus, output string, findings []ComparisonFinding, failure string, at time.Time) (Comparison, error) {
	if want.IsZero() || workspace.IsZero() || actor.IsZero() {
		return Comparison{}, ErrIDRequired
	}
	if len(observations) == 0 {
		return Comparison{}, ErrSynthesisRequired
	}
	if len(observations) > MaxComparisonObservations {
		return Comparison{}, ErrSynthesisTooLarge
	}
	seen := make(map[id.ID]struct{}, len(observations))
	for _, observation := range observations {
		if observation.IsZero() {
			return Comparison{}, ErrIDRequired
		}
		if _, exists := seen[observation]; exists {
			return Comparison{}, ErrCandidateInvalid
		}
		seen[observation] = struct{}{}
	}
	if len([]rune(strings.TrimSpace(provider))) == 0 || len(provider) > 200 || !utf8.ValidString(provider) {
		return Comparison{}, ErrProviderRequired
	}
	if len([]rune(strings.TrimSpace(method))) == 0 || len(method) > 200 || !utf8.ValidString(method) {
		return Comparison{}, ErrMethodRequired
	}
	if len([]rune(strings.TrimSpace(templateVersion))) == 0 || len(templateVersion) > 200 || !utf8.ValidString(templateVersion) {
		return Comparison{}, ErrMethodRequired
	}
	parsedStatus, err := ParseComparisonStatus(status.String())
	if err != nil {
		return Comparison{}, err
	}
	if parsedStatus == ComparisonCompleted || parsedStatus == ComparisonEmpty {
		if !utf8.ValidString(output) || strings.ContainsRune(output, 0) || len(output) > MaxComparisonOutput {
			return Comparison{}, ErrSynthesisOutput
		}
		if strings.TrimSpace(failure) != "" {
			return Comparison{}, ErrSynthesisFailure
		}
	} else {
		if strings.TrimSpace(output) != "" || len(findings) != 0 {
			return Comparison{}, ErrSynthesisFailure
		}
		if strings.TrimSpace(failure) == "" || !utf8.ValidString(failure) || strings.ContainsRune(failure, 0) || len(failure) > MaxComparisonError {
			return Comparison{}, ErrSynthesisFailure
		}
	}
	if len(findings) > MaxComparisonFindings {
		return Comparison{}, ErrSynthesisOutput
	}
	cleanFindings := make([]ComparisonFinding, 0, len(findings))
	for _, finding := range findings {
		kind, err := ParseFindingKind(finding.Kind.String())
		if err != nil {
			return Comparison{}, err
		}
		summary := strings.TrimSpace(finding.Summary)
		if summary == "" || len(summary) > MaxComparisonSummary || !utf8.ValidString(summary) || strings.ContainsRune(summary, 0) {
			return Comparison{}, ErrSynthesisOutput
		}
		if len(finding.ObservationIDs) == 0 {
			return Comparison{}, ErrCandidateInvalid
		}
		findingSeen := make(map[id.ID]struct{}, len(finding.ObservationIDs))
		citationIDs := make([]id.ID, 0, len(finding.ObservationIDs))
		for _, observation := range finding.ObservationIDs {
			if _, exists := seen[observation]; !exists {
				return Comparison{}, ErrCandidateInvalid
			}
			if _, exists := findingSeen[observation]; exists {
				return Comparison{}, ErrCandidateInvalid
			}
			findingSeen[observation] = struct{}{}
			citationIDs = append(citationIDs, observation)
		}
		cleanFindings = append(cleanFindings, ComparisonFinding{Kind: kind, Summary: summary, ObservationIDs: citationIDs})
	}
	if at.IsZero() {
		return Comparison{}, ErrTimeRequired
	}
	return Comparison{ID: want, WorkspaceID: workspace, ObservationIDs: append([]id.ID(nil), observations...), Provider: strings.TrimSpace(provider), Method: strings.TrimSpace(method), TemplateVersion: strings.TrimSpace(templateVersion), Status: parsedStatus, Output: output, Findings: cleanFindings, CreatedBy: actor, CreatedAt: at, Error: strings.TrimSpace(failure)}, nil
}
