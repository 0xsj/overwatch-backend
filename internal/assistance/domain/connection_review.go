package domain

import (
	"strings"
	"time"
	"unicode/utf8"

	connectiondomain "github.com/0xsj/overwatch-backend/internal/researchconnection/domain"
	"github.com/0xsj/overwatch-backend/pkg/errors"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

const (
	MaxConnectionReviewObservations = 24
	MaxConnectionReviewFindings     = 24
	MaxConnectionReviewOutput       = 24000
	MaxConnectionReviewSummary      = 4000
)

const EventConnectionReviewGenerated = "assistance.connection_review.generated"

type ConnectionReviewStatus string

const (
	ConnectionReviewCompleted ConnectionReviewStatus = "completed"
	ConnectionReviewEmpty     ConnectionReviewStatus = "empty"
)

func (s ConnectionReviewStatus) String() string { return string(s) }

func ParseConnectionReviewStatus(raw string) (ConnectionReviewStatus, error) {
	switch ConnectionReviewStatus(strings.TrimSpace(raw)) {
	case ConnectionReviewCompleted, ConnectionReviewEmpty:
		return ConnectionReviewStatus(strings.TrimSpace(raw)), nil
	default:
		return "", errors.New(errors.Invalid, "connection review status must be completed or empty")
	}
}

type ConnectionReviewFindingKind string

const (
	ConnectionSupport                ConnectionReviewFindingKind = "support"
	ConnectionOpposition             ConnectionReviewFindingKind = "opposition"
	ConnectionAlternative            ConnectionReviewFindingKind = "alternative"
	ConnectionDiscriminatingEvidence ConnectionReviewFindingKind = "discriminating_evidence"
)

func (k ConnectionReviewFindingKind) String() string { return string(k) }

func ParseConnectionReviewFindingKind(raw string) (ConnectionReviewFindingKind, error) {
	switch ConnectionReviewFindingKind(strings.TrimSpace(raw)) {
	case ConnectionSupport, ConnectionOpposition, ConnectionAlternative, ConnectionDiscriminatingEvidence:
		return ConnectionReviewFindingKind(strings.TrimSpace(raw)), nil
	default:
		return "", errors.New(errors.Invalid, "connection review finding kind is unknown")
	}
}

type ConnectionReviewFinding struct {
	Kind           ConnectionReviewFindingKind `json:"kind"`
	Summary        string                      `json:"summary"`
	ObservationIDs []id.ID                     `json:"observation_ids"`
}

// ConnectionReview is an assistant proposal over an authored connection. The
// authored connection remains the source of truth; this record preserves the
// exact relationship and citations that were reviewed at generation time.
type ConnectionReview struct {
	ID                       id.ID                     `json:"connection_review_id"`
	WorkspaceID              id.ID                     `json:"workspace_id"`
	ConnectionID             id.ID                     `json:"connection_id"`
	FromRecordID             id.ID                     `json:"from_record_id"`
	ToRecordID               id.ID                     `json:"to_record_id"`
	ConnectionKind           connectiondomain.Kind     `json:"connection_kind"`
	ConnectionState          connectiondomain.State    `json:"connection_state"`
	ConnectionRationale      string                    `json:"connection_rationale"`
	SupportingObservationIDs []id.ID                   `json:"supporting_observation_ids"`
	OpposingObservationIDs   []id.ID                   `json:"opposing_observation_ids"`
	Provider                 string                    `json:"provider"`
	Method                   string                    `json:"method"`
	TemplateVersion          string                    `json:"template_version"`
	Status                   ConnectionReviewStatus    `json:"status"`
	Output                   string                    `json:"output"`
	Findings                 []ConnectionReviewFinding `json:"findings"`
	CreatedBy                id.ID                     `json:"created_by"`
	CreatedAt                time.Time                 `json:"created_at"`
}

func NewConnectionReview(want, workspace, actor, connectionID, fromRecordID, toRecordID id.ID, kind, state, rationale string, supporting, opposing []id.ID, provider, method, templateVersion string, status ConnectionReviewStatus, output string, findings []ConnectionReviewFinding, at time.Time) (ConnectionReview, error) {
	if want.IsZero() || workspace.IsZero() || actor.IsZero() || connectionID.IsZero() || fromRecordID.IsZero() || toRecordID.IsZero() {
		return ConnectionReview{}, ErrIDRequired
	}
	if fromRecordID == toRecordID {
		return ConnectionReview{}, ErrCandidateInvalid
	}
	parsedKind, err := connectiondomain.ParseKind(kind)
	if err != nil {
		return ConnectionReview{}, err
	}
	parsedState, err := connectiondomain.ParseState(state)
	if err != nil {
		return ConnectionReview{}, err
	}
	rationale = strings.TrimSpace(rationale)
	if rationale == "" || len(rationale) > connectiondomain.MaxRationale || !utf8.ValidString(rationale) || strings.ContainsRune(rationale, 0) {
		return ConnectionReview{}, ErrConnectionReviewRationale
	}
	supporting, err = reviewEvidenceIDs(supporting)
	if err != nil {
		return ConnectionReview{}, err
	}
	opposing, err = reviewEvidenceIDs(opposing)
	if err != nil {
		return ConnectionReview{}, err
	}
	if reviewOverlap(supporting, opposing) {
		return ConnectionReview{}, ErrCandidateInvalid
	}
	if len(supporting)+len(opposing) > MaxConnectionReviewObservations {
		return ConnectionReview{}, ErrConnectionReviewTooLarge
	}
	if len(supporting)+len(opposing) == 0 {
		return ConnectionReview{}, ErrConnectionReviewRequired
	}
	if len([]rune(strings.TrimSpace(provider))) == 0 || len(provider) > 200 || !utf8.ValidString(provider) {
		return ConnectionReview{}, ErrProviderRequired
	}
	if len([]rune(strings.TrimSpace(method))) == 0 || len(method) > 200 || !utf8.ValidString(method) {
		return ConnectionReview{}, ErrMethodRequired
	}
	if len([]rune(strings.TrimSpace(templateVersion))) == 0 || len(templateVersion) > 200 || !utf8.ValidString(templateVersion) {
		return ConnectionReview{}, ErrMethodRequired
	}
	parsedStatus, err := ParseConnectionReviewStatus(status.String())
	if err != nil {
		return ConnectionReview{}, err
	}
	if !utf8.ValidString(output) || strings.ContainsRune(output, 0) || len(output) > MaxConnectionReviewOutput {
		return ConnectionReview{}, ErrConnectionReviewOutput
	}
	if len(findings) > MaxConnectionReviewFindings {
		return ConnectionReview{}, ErrConnectionReviewOutput
	}
	selected := make(map[id.ID]struct{}, len(supporting)+len(opposing))
	for _, observation := range append(append([]id.ID{}, supporting...), opposing...) {
		selected[observation] = struct{}{}
	}
	cleanFindings := make([]ConnectionReviewFinding, 0, len(findings))
	for _, finding := range findings {
		kind, err := ParseConnectionReviewFindingKind(finding.Kind.String())
		if err != nil {
			return ConnectionReview{}, err
		}
		summary := strings.TrimSpace(finding.Summary)
		if summary == "" || len(summary) > MaxConnectionReviewSummary || !utf8.ValidString(summary) || strings.ContainsRune(summary, 0) {
			return ConnectionReview{}, ErrConnectionReviewOutput
		}
		if len(finding.ObservationIDs) == 0 {
			return ConnectionReview{}, ErrCandidateInvalid
		}
		seen := make(map[id.ID]struct{}, len(finding.ObservationIDs))
		citations := make([]id.ID, 0, len(finding.ObservationIDs))
		for _, observation := range finding.ObservationIDs {
			if _, ok := selected[observation]; !ok {
				return ConnectionReview{}, ErrCandidateInvalid
			}
			if _, ok := seen[observation]; ok {
				return ConnectionReview{}, ErrCandidateInvalid
			}
			seen[observation] = struct{}{}
			citations = append(citations, observation)
		}
		cleanFindings = append(cleanFindings, ConnectionReviewFinding{Kind: kind, Summary: summary, ObservationIDs: citations})
	}
	if at.IsZero() {
		return ConnectionReview{}, ErrTimeRequired
	}
	return ConnectionReview{
		ID: want, WorkspaceID: workspace, ConnectionID: connectionID, FromRecordID: fromRecordID, ToRecordID: toRecordID,
		ConnectionKind: parsedKind, ConnectionState: parsedState, ConnectionRationale: rationale,
		SupportingObservationIDs: supporting, OpposingObservationIDs: opposing,
		Provider: strings.TrimSpace(provider), Method: strings.TrimSpace(method), TemplateVersion: strings.TrimSpace(templateVersion),
		Status: parsedStatus, Output: output, Findings: cleanFindings, CreatedBy: actor, CreatedAt: at,
	}, nil
}

func reviewEvidenceIDs(input []id.ID) ([]id.ID, error) {
	if len(input) > connectiondomain.MaxEvidencePerSide {
		return nil, ErrConnectionReviewTooLarge
	}
	seen := make(map[id.ID]struct{}, len(input))
	out := make([]id.ID, 0, len(input))
	for _, observation := range input {
		if observation.IsZero() {
			return nil, ErrIDRequired
		}
		if _, ok := seen[observation]; ok {
			return nil, ErrCandidateInvalid
		}
		seen[observation] = struct{}{}
		out = append(out, observation)
	}
	return out, nil
}

func reviewOverlap(left, right []id.ID) bool {
	seen := make(map[id.ID]struct{}, len(left))
	for _, observation := range left {
		seen[observation] = struct{}{}
	}
	for _, observation := range right {
		if _, ok := seen[observation]; ok {
			return true
		}
	}
	return false
}
