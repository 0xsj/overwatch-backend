package app

import (
	"context"
	"fmt"
	"strings"

	"github.com/0xsj/overwatch-backend/internal/assistance/domain"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

type BriefDraftObservation struct {
	ID          id.ID
	SourceTitle string
	Statement   string
	Quote       string
}

type BriefDraftInput struct {
	Brief        domain.BriefDraftInput
	Observations []BriefDraftObservation
}

type BriefDraftProvider interface {
	DraftBrief(context.Context, BriefDraftInput) (domain.BriefDraftStatus, string, []domain.BriefDraftChange, error)
	Name() string
	Method() string
	TemplateVersion() string
}

// LocalBriefDraftProvider produces an explicit evidence-notes diff. It does
// not assert what the observations mean and never mutates the working brief.
type LocalBriefDraftProvider struct{}

func (LocalBriefDraftProvider) Name() string            { return "local" }
func (LocalBriefDraftProvider) Method() string          { return "working-brief-diff-v1" }
func (LocalBriefDraftProvider) TemplateVersion() string { return "brief-draft-v1" }
func (LocalBriefDraftProvider) External() bool          { return false }

func (LocalBriefDraftProvider) DraftBrief(ctx context.Context, input BriefDraftInput) (domain.BriefDraftStatus, string, []domain.BriefDraftChange, error) {
	if err := ctx.Err(); err != nil {
		return "", "", nil, err
	}
	if len(input.Observations) == 0 {
		return domain.BriefDraftEmpty, "No selected observations were available for a brief draft.", nil, nil
	}
	ids := make([]id.ID, 0, len(input.Observations))
	lines := make([]string, 0, len(input.Observations))
	for _, observation := range input.Observations {
		ids = append(ids, observation.ID)
		statement := truncateBriefDraft(observation.Statement, 500)
		if statement == "" {
			statement = truncateBriefDraft(observation.Quote, 500)
		}
		lines = append(lines, fmt.Sprintf("- %s: %s", strings.TrimSpace(observation.SourceTitle), statement))
	}
	evidenceNotes := "Proposed evidence notes (review before saving):\n" + strings.Join(lines, "\n")
	accountAfter := appendBriefDraft(input.Brief.CurrentAccount, evidenceNotes)
	limitationsAfter := appendBriefDraft(input.Brief.Limitations, "Proposed limitation: the selected citations are preserved here for review but have not been independently adjudicated by this assistant.")
	nextStepsAfter := appendBriefDraft(input.Brief.NextSteps, "Proposed next step: compare the selected citations together, record an analyst rationale, and update the authored brief only after review.")
	changes := []domain.BriefDraftChange{
		{Section: domain.BriefDraftCurrentAccount, Before: input.Brief.CurrentAccount, After: accountAfter, Rationale: "Keep the selected evidence visible beside the authored account without asserting that it proves the account.", ObservationIDs: ids},
		{Section: domain.BriefDraftLimitations, Before: input.Brief.Limitations, After: limitationsAfter, Rationale: "Make the review boundary explicit while the selected evidence is being assessed.", ObservationIDs: ids},
		{Section: domain.BriefDraftNextSteps, Before: input.Brief.NextSteps, After: nextStepsAfter, Rationale: "Turn the selected citations into a concrete analyst review step without saving it automatically.", ObservationIDs: ids},
	}
	var output strings.Builder
	output.WriteString("Assisted working-brief draft\n\n")
	output.WriteString("This is a proposed diff over the authored brief. It preserves selected evidence as review notes and does not establish a conclusion.\n")
	for _, change := range changes {
		output.WriteString("\n## ")
		output.WriteString(change.Section.String())
		output.WriteString("\nBefore:\n")
		output.WriteString(change.Before)
		output.WriteString("\nAfter:\n")
		output.WriteString(change.After)
		output.WriteString("\n")
	}
	return domain.BriefDraftCompleted, output.String(), changes, nil
}

func appendBriefDraft(before, addition string) string {
	before, addition = strings.TrimSpace(before), strings.TrimSpace(addition)
	if before == "" {
		return addition
	}
	return before + "\n\n" + addition
}

func truncateBriefDraft(value string, max int) string {
	value = strings.TrimSpace(value)
	if len([]rune(value)) <= max {
		return value
	}
	return string([]rune(value)[:max]) + "…"
}
