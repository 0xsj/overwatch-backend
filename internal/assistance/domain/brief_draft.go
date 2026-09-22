package domain

import (
	"strings"
	"time"
	"unicode/utf8"

	"github.com/0xsj/overwatch-backend/pkg/errors"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

const (
	MaxBriefDraftObservations = 12
	MaxBriefDraftChanges      = 6
	MaxBriefDraftOutput       = 24000
	MaxBriefDraftText         = 8000
	MaxBriefDraftRationale    = 2000
	MaxBriefDraftError        = 2000
)

const EventBriefDraftGenerated = "assistance.brief_draft.generated"

type BriefDraftStatus string

const (
	BriefDraftCompleted   BriefDraftStatus = "completed"
	BriefDraftEmpty       BriefDraftStatus = "empty"
	BriefDraftFailed      BriefDraftStatus = "failed"
	BriefDraftUnsupported BriefDraftStatus = "unsupported"
	BriefDraftTimedOut    BriefDraftStatus = "timed_out"
)

func (s BriefDraftStatus) String() string { return string(s) }

func ParseBriefDraftStatus(raw string) (BriefDraftStatus, error) {
	switch BriefDraftStatus(strings.TrimSpace(raw)) {
	case BriefDraftCompleted, BriefDraftEmpty, BriefDraftFailed, BriefDraftUnsupported, BriefDraftTimedOut:
		return BriefDraftStatus(strings.TrimSpace(raw)), nil
	default:
		return "", errors.New(errors.Invalid, "brief draft status is unknown")
	}
}

type BriefDraftSection string

const (
	BriefDraftTitle          BriefDraftSection = "title"
	BriefDraftQuestion       BriefDraftSection = "question"
	BriefDraftCurrentAccount BriefDraftSection = "current_account"
	BriefDraftAlternatives   BriefDraftSection = "alternatives"
	BriefDraftLimitations    BriefDraftSection = "limitations"
	BriefDraftNextSteps      BriefDraftSection = "next_steps"
)

func (s BriefDraftSection) String() string { return string(s) }

func ParseBriefDraftSection(raw string) (BriefDraftSection, error) {
	switch BriefDraftSection(strings.TrimSpace(raw)) {
	case BriefDraftTitle, BriefDraftQuestion, BriefDraftCurrentAccount, BriefDraftAlternatives, BriefDraftLimitations, BriefDraftNextSteps:
		return BriefDraftSection(strings.TrimSpace(raw)), nil
	default:
		return "", ErrBriefDraftSection
	}
}

// BriefDraftInput snapshots the authored text and the exact observations used
// for a proposal. It is intentionally separate from brief.Brief so assistance
// cannot mutate the authored brief through a shared model.
type BriefDraftInput struct {
	BriefID        id.ID   `json:"brief_id"`
	Title          string  `json:"title"`
	Question       string  `json:"question"`
	CurrentAccount string  `json:"current_account,omitempty"`
	Alternatives   string  `json:"alternatives,omitempty"`
	Limitations    string  `json:"limitations,omitempty"`
	NextSteps      string  `json:"next_steps,omitempty"`
	ObservationIDs []id.ID `json:"observation_ids"`
}

type BriefDraftChange struct {
	Section        BriefDraftSection `json:"section"`
	Before         string            `json:"before"`
	After          string            `json:"after"`
	Rationale      string            `json:"rationale"`
	ObservationIDs []id.ID           `json:"observation_ids"`
}

type BriefDraft struct {
	ID              id.ID              `json:"brief_draft_id"`
	WorkspaceID     id.ID              `json:"workspace_id"`
	Input           BriefDraftInput    `json:"input"`
	Provider        string             `json:"provider"`
	Method          string             `json:"method"`
	TemplateVersion string             `json:"template_version"`
	Status          BriefDraftStatus   `json:"status"`
	Output          string             `json:"output"`
	Changes         []BriefDraftChange `json:"changes"`
	CreatedBy       id.ID              `json:"created_by"`
	CreatedAt       time.Time          `json:"created_at"`
	Error           string             `json:"error,omitempty"`
}

func NewBriefDraft(want, workspace, actor id.ID, input BriefDraftInput, provider, method, templateVersion string, status BriefDraftStatus, output string, changes []BriefDraftChange, at time.Time) (BriefDraft, error) {
	return NewBriefDraftResult(want, workspace, actor, input, provider, method, templateVersion, status, output, changes, "", at)
}

func NewBriefDraftResult(want, workspace, actor id.ID, input BriefDraftInput, provider, method, templateVersion string, status BriefDraftStatus, output string, changes []BriefDraftChange, failure string, at time.Time) (BriefDraft, error) {
	if want.IsZero() || workspace.IsZero() || actor.IsZero() || input.BriefID.IsZero() {
		return BriefDraft{}, ErrBriefDraftRequired
	}
	if len(input.ObservationIDs) == 0 {
		return BriefDraft{}, ErrBriefDraftRequired
	}
	if len(input.ObservationIDs) > MaxBriefDraftObservations {
		return BriefDraft{}, ErrBriefDraftTooLarge
	}
	seen := make(map[id.ID]struct{}, len(input.ObservationIDs))
	for _, observation := range input.ObservationIDs {
		if observation.IsZero() {
			return BriefDraft{}, ErrIDRequired
		}
		if _, exists := seen[observation]; exists {
			return BriefDraft{}, ErrCandidateInvalid
		}
		seen[observation] = struct{}{}
	}
	if !validBriefDraftText(input.Title) || !validBriefDraftText(input.Question) || !validBriefDraftText(input.CurrentAccount) || !validBriefDraftText(input.Alternatives) || !validBriefDraftText(input.Limitations) || !validBriefDraftText(input.NextSteps) {
		return BriefDraft{}, ErrBriefDraftRequired
	}
	if len([]rune(strings.TrimSpace(provider))) == 0 || len(provider) > 200 || !utf8.ValidString(provider) {
		return BriefDraft{}, ErrProviderRequired
	}
	if len([]rune(strings.TrimSpace(method))) == 0 || len(method) > 200 || !utf8.ValidString(method) {
		return BriefDraft{}, ErrMethodRequired
	}
	if len([]rune(strings.TrimSpace(templateVersion))) == 0 || len(templateVersion) > 200 || !utf8.ValidString(templateVersion) {
		return BriefDraft{}, ErrMethodRequired
	}
	parsedStatus, err := ParseBriefDraftStatus(status.String())
	if err != nil {
		return BriefDraft{}, err
	}
	if parsedStatus == BriefDraftCompleted || parsedStatus == BriefDraftEmpty {
		if !utf8.ValidString(output) || strings.ContainsRune(output, 0) || len(output) > MaxBriefDraftOutput || strings.TrimSpace(failure) != "" {
			return BriefDraft{}, ErrBriefDraftOutput
		}
	} else if strings.TrimSpace(output) != "" || len(changes) != 0 || strings.TrimSpace(failure) == "" || !utf8.ValidString(failure) || strings.ContainsRune(failure, 0) || len(failure) > MaxBriefDraftError {
		return BriefDraft{}, ErrBriefDraftOutput
	}
	if len(changes) > MaxBriefDraftChanges {
		return BriefDraft{}, ErrBriefDraftOutput
	}
	cleanChanges := make([]BriefDraftChange, 0, len(changes))
	seenSections := make(map[BriefDraftSection]struct{}, len(changes))
	for _, change := range changes {
		section, err := ParseBriefDraftSection(change.Section.String())
		if err != nil {
			return BriefDraft{}, err
		}
		if _, exists := seenSections[section]; exists {
			return BriefDraft{}, ErrCandidateInvalid
		}
		seenSections[section] = struct{}{}
		before, after, rationale := strings.TrimSpace(change.Before), strings.TrimSpace(change.After), strings.TrimSpace(change.Rationale)
		if !validBriefDraftText(before) || !validBriefDraftText(after) || after == before || rationale == "" || len(rationale) > MaxBriefDraftRationale || !utf8.ValidString(rationale) || strings.ContainsRune(rationale, 0) {
			return BriefDraft{}, ErrBriefDraftOutput
		}
		citations, err := draftObservationIDs(change.ObservationIDs, seen)
		if err != nil {
			return BriefDraft{}, err
		}
		if len(citations) == 0 {
			return BriefDraft{}, ErrBriefDraftOutput
		}
		cleanChanges = append(cleanChanges, BriefDraftChange{Section: section, Before: before, After: after, Rationale: rationale, ObservationIDs: citations})
	}
	if parsedStatus == BriefDraftCompleted && len(cleanChanges) == 0 {
		return BriefDraft{}, ErrBriefDraftOutput
	}
	if at.IsZero() {
		return BriefDraft{}, ErrTimeRequired
	}
	cleanInput := input
	cleanInput.Title, cleanInput.Question = strings.TrimSpace(input.Title), strings.TrimSpace(input.Question)
	cleanInput.CurrentAccount, cleanInput.Alternatives = strings.TrimSpace(input.CurrentAccount), strings.TrimSpace(input.Alternatives)
	cleanInput.Limitations, cleanInput.NextSteps = strings.TrimSpace(input.Limitations), strings.TrimSpace(input.NextSteps)
	cleanInput.ObservationIDs = append([]id.ID(nil), input.ObservationIDs...)
	return BriefDraft{ID: want, WorkspaceID: workspace, Input: cleanInput, Provider: strings.TrimSpace(provider), Method: strings.TrimSpace(method), TemplateVersion: strings.TrimSpace(templateVersion), Status: parsedStatus, Output: output, Changes: cleanChanges, CreatedBy: actor, CreatedAt: at, Error: strings.TrimSpace(failure)}, nil
}

func validBriefDraftText(value string) bool {
	return utf8.ValidString(value) && !strings.ContainsRune(value, 0) && len(value) <= MaxBriefDraftText
}

func draftObservationIDs(input []id.ID, selected map[id.ID]struct{}) ([]id.ID, error) {
	if len(input) > MaxBriefDraftObservations {
		return nil, ErrBriefDraftTooLarge
	}
	seen := make(map[id.ID]struct{}, len(input))
	out := make([]id.ID, 0, len(input))
	for _, observation := range input {
		if observation.IsZero() {
			return nil, ErrIDRequired
		}
		if _, duplicate := seen[observation]; duplicate {
			return nil, ErrCandidateInvalid
		}
		if _, allowed := selected[observation]; !allowed {
			return nil, ErrCandidateInvalid
		}
		seen[observation] = struct{}{}
		out = append(out, observation)
	}
	return out, nil
}
