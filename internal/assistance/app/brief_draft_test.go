package app

import (
	"context"
	"testing"
	"time"

	"github.com/0xsj/overwatch-backend/internal/assistance/domain"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

func TestLocalBriefDraftProviderProducesCitedReviewableChanges(t *testing.T) {
	briefID := id.ID{1}
	firstObservation := id.ID{2}
	secondObservation := id.ID{3}
	provider := LocalBriefDraftProvider{}
	input := BriefDraftInput{
		Brief: domain.BriefDraftInput{
			BriefID:        briefID,
			Title:          "East Quay handoff",
			Question:       "What happened at East Quay?",
			CurrentAccount: "The notice describes a disruption.",
			Limitations:    "Only one source is currently retained.",
			NextSteps:      "Find an independent account.",
			ObservationIDs: []id.ID{firstObservation, secondObservation},
		},
		Observations: []BriefDraftObservation{
			{ID: firstObservation, SourceTitle: "Notice", Statement: "A disruption was reported at East Quay."},
			{ID: secondObservation, SourceTitle: "Log", Statement: "The location was recorded as East Quay."},
		},
	}

	status, output, changes, err := provider.DraftBrief(context.Background(), input)
	if err != nil {
		t.Fatalf("draft brief: %v", err)
	}
	if status != domain.BriefDraftCompleted || output == "" || len(changes) != 3 {
		t.Fatalf("unexpected draft result: status=%q output=%q changes=%d", status, output, len(changes))
	}
	for _, change := range changes {
		if len(change.ObservationIDs) != 2 || change.ObservationIDs[0] != firstObservation || change.ObservationIDs[1] != secondObservation {
			t.Fatalf("change lost exact citations: %+v", change)
		}
		if change.Before == change.After || change.Rationale == "" {
			t.Fatalf("change was not reviewable: %+v", change)
		}
	}
	if changes[0].Section != domain.BriefDraftCurrentAccount || changes[0].Before != input.Brief.CurrentAccount {
		t.Fatalf("provider changed the wrong authored section: %+v", changes[0])
	}

	draft, err := domain.NewBriefDraft(id.ID{4}, id.ID{5}, id.ID{6}, input.Brief, provider.Name(), provider.Method(), provider.TemplateVersion(), status, output, changes, time.Date(2026, 9, 19, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("validate generated draft: %v", err)
	}
	if draft.Input.BriefID != briefID || len(draft.Input.ObservationIDs) != 2 {
		t.Fatalf("validated draft lost input snapshot: %+v", draft.Input)
	}
}
