package app_test

import (
	"context"
	"errors"
	"testing"

	"github.com/0xsj/overwatch-backend/internal/assistance/app"
	pkgerrors "github.com/0xsj/overwatch-backend/pkg/errors"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

func TestProcessProviderPassesRetainedInputAndReadsCitableJSON(t *testing.T) {
	provider := app.NewProcessProvider(app.ProcessProviderConfig{
		Binary: "/bin/sh",
		Args:   []string{"-c", `grep -q '"content":"East Quay is named\."' "$1" && printf '[{"statement":"East Quay is named.","quote":"East Quay","quote_start":0,"candidate_kind":"place","candidate_name":"East Quay","candidate_description":"A place-shaped candidate for review.","relationship_kind":"located_at","related_candidate_kind":"account","related_candidate_name":"@eastquay","relationship_description":"The passage places the account at the named location."}]'`, "assistance-test", app.ProcessInputPlaceholder},
	})
	source, capture, extraction := id.ID{1}, id.ID{2}, id.ID{3}
	got, err := provider.Extract(context.Background(), app.Input{SourceID: source, CaptureID: capture, ExtractionID: extraction, Content: "East Quay is named."})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Statement != "East Quay is named." || got[0].Quote != "East Quay" || got[0].QuoteStart != 0 || got[0].QuoteEnd != 9 || got[0].CandidateKind != "place" || got[0].CandidateName != "East Quay" || got[0].CandidateDescription == "" || got[0].RelationshipKind != "located_at" || got[0].RelatedCandidateKind != "account" || got[0].RelatedCandidateName != "@eastquay" || got[0].RelationshipDescription == "" {
		t.Fatalf("unexpected process proposal: %+v", got)
	}
}

func TestProcessProviderMissingBinaryIsExplicitlyUnavailable(t *testing.T) {
	provider := app.NewProcessProvider(app.ProcessProviderConfig{Binary: "not-a-real-overwatch-assistance-provider"})
	_, err := provider.Extract(context.Background(), app.Input{Content: "source"})
	if !errors.Is(err, app.ErrProcessProviderUnavailable) {
		t.Fatalf("error=%v, want ErrProcessProviderUnavailable", err)
	}
	if got := pkgerrors.KindOf(err); got != pkgerrors.Unavailable {
		t.Fatalf("error kind=%s, want unavailable", got)
	}
}
