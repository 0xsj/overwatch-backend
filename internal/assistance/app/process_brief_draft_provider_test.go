package app_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/0xsj/overwatch-backend/internal/assistance/app"
	"github.com/0xsj/overwatch-backend/internal/assistance/domain"
	pkgerrors "github.com/0xsj/overwatch-backend/pkg/errors"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

func TestProcessBriefDraftProviderReadsGroundedJSON(t *testing.T) {
	brief, observation := id.ID{1}, id.ID{2}
	provider := app.NewProcessBriefDraftProvider(app.BriefDraftProcessProviderConfig{
		Binary: "/bin/sh",
		Args:   []string{"-c", fmt.Sprintf(`grep -q '"title":"East Quay"' "$1" && printf '{"status":"completed","output":"Reviewable brief diff.","changes":[{"section":"current_account","before":"The notice describes a disruption.","after":"The notice describes a disruption and preserves the selected citation for review.","rationale":"Keep the evidence visible without asserting it proves the account.","observation_ids":["%s"]}]}'`, observation.String()), "brief-test", app.BriefDraftInputPlaceholder},
	})
	status, output, changes, err := provider.DraftBrief(context.Background(), app.BriefDraftInput{
		Brief:        domain.BriefDraftInput{BriefID: brief, Title: "East Quay", Question: "What happened?", CurrentAccount: "The notice describes a disruption.", ObservationIDs: []id.ID{observation}},
		Observations: []app.BriefDraftObservation{{ID: observation, SourceTitle: "Notice", Statement: "A disruption was reported."}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if status != domain.BriefDraftCompleted || output == "" || len(changes) != 1 || changes[0].ObservationIDs[0] != observation || changes[0].Section != domain.BriefDraftCurrentAccount {
		t.Fatalf("unexpected brief draft: status=%q output=%q changes=%+v", status, output, changes)
	}
}

func TestProcessBriefDraftProviderRejectsUnknownFields(t *testing.T) {
	provider := app.NewProcessBriefDraftProvider(app.BriefDraftProcessProviderConfig{Binary: "/bin/sh", Args: []string{"-c", "printf '%s' \"$1\"", "brief-test", `{"status":"completed","output":"review","changes":[],"extra":true}`, app.BriefDraftInputPlaceholder}, MaxOutput: 1024})
	_, _, _, err := provider.DraftBrief(context.Background(), app.BriefDraftInput{Brief: domain.BriefDraftInput{BriefID: id.ID{1}, Title: "East Quay", Question: "What happened?", ObservationIDs: []id.ID{{2}}}})
	if err == nil {
		t.Fatal("expected provider output to be rejected")
	}
}

func TestProcessBriefDraftProviderMissingBinaryIsExplicitlyUnavailable(t *testing.T) {
	provider := app.NewProcessBriefDraftProvider(app.BriefDraftProcessProviderConfig{Binary: "not-a-real-overwatch-brief-provider"})
	_, _, _, err := provider.DraftBrief(context.Background(), app.BriefDraftInput{Brief: domain.BriefDraftInput{BriefID: id.ID{1}, Title: "East Quay", Question: "What happened?", ObservationIDs: []id.ID{{2}}}})
	if !errors.Is(err, app.ErrProcessProviderUnavailable) {
		t.Fatalf("error=%v, want unavailable provider error", err)
	}
	if got := pkgerrors.KindOf(err); got != pkgerrors.Unavailable {
		t.Fatalf("error kind=%s, want unavailable", got)
	}
}
