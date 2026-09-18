package domain

import (
	"strings"
	"time"
	"unicode/utf8"

	"github.com/0xsj/overwatch-backend/pkg/id"
)

const (
	MaxSynthesisObservations = 6
	MaxSynthesisOutput       = 24000
	MaxSynthesisCandidates   = 50
)

const EventSynthesisGenerated = "assistance.synthesis.generated"

// SynthesisCandidate is a narrow, reviewable output. It points back to the
// selected observations that exposed the identifier; it is not a research
// record and carries no identity decision.
type SynthesisCandidate struct {
	Kind           string  `json:"kind"`
	Name           string  `json:"name"`
	ObservationIDs []id.ID `json:"observation_ids"`
	Rationale      string  `json:"rationale"`
}

type Synthesis struct {
	ID             id.ID                `json:"synthesis_id"`
	WorkspaceID    id.ID                `json:"workspace_id"`
	ObservationIDs []id.ID              `json:"observation_ids"`
	Provider       string               `json:"provider"`
	Method         string               `json:"method"`
	Output         string               `json:"output"`
	Candidates     []SynthesisCandidate `json:"candidates"`
	CreatedBy      id.ID                `json:"created_by"`
	CreatedAt      time.Time            `json:"created_at"`
}

func NewSynthesis(want, workspace, actor id.ID, observations []id.ID, provider, method, output string, candidates []SynthesisCandidate, at time.Time) (Synthesis, error) {
	if want.IsZero() || actor.IsZero() {
		return Synthesis{}, ErrIDRequired
	}
	if workspace.IsZero() {
		return Synthesis{}, ErrWorkspaceRequired
	}
	if len(observations) == 0 {
		return Synthesis{}, ErrSynthesisRequired
	}
	if len(observations) > MaxSynthesisObservations {
		return Synthesis{}, ErrSynthesisTooLarge
	}
	seen := make(map[id.ID]struct{}, len(observations))
	for _, observation := range observations {
		if observation.IsZero() {
			return Synthesis{}, ErrIDRequired
		}
		if _, exists := seen[observation]; exists {
			return Synthesis{}, ErrCandidateInvalid
		}
		seen[observation] = struct{}{}
	}
	if at.IsZero() || !utf8.ValidString(output) || strings.ContainsRune(output, 0) || len(output) > MaxSynthesisOutput {
		return Synthesis{}, ErrSynthesisOutput
	}
	if len(candidates) > MaxSynthesisCandidates {
		return Synthesis{}, ErrSynthesisOutput
	}
	for _, candidate := range candidates {
		if candidate.Kind != "account" || strings.TrimSpace(candidate.Name) == "" || len(candidate.Name) > 400 || !utf8.ValidString(candidate.Name) || len(candidate.ObservationIDs) == 0 || len(candidate.Rationale) > 4000 || !utf8.ValidString(candidate.Rationale) {
			return Synthesis{}, ErrCandidateInvalid
		}
		candidateSeen := make(map[id.ID]struct{}, len(candidate.ObservationIDs))
		for _, observation := range candidate.ObservationIDs {
			if _, exists := seen[observation]; !exists {
				return Synthesis{}, ErrCandidateInvalid
			}
			if _, exists := candidateSeen[observation]; exists {
				return Synthesis{}, ErrCandidateInvalid
			}
			candidateSeen[observation] = struct{}{}
		}
	}
	if strings.TrimSpace(provider) == "" {
		return Synthesis{}, ErrProviderRequired
	}
	if strings.TrimSpace(method) == "" {
		return Synthesis{}, ErrMethodRequired
	}
	return Synthesis{ID: want, WorkspaceID: workspace, ObservationIDs: append([]id.ID(nil), observations...), Provider: strings.TrimSpace(provider), Method: strings.TrimSpace(method), Output: output, Candidates: append([]SynthesisCandidate(nil), candidates...), CreatedBy: actor, CreatedAt: at}, nil
}
