package app_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/0xsj/overwatch-backend/internal/assistance/app"
	pkgerrors "github.com/0xsj/overwatch-backend/pkg/errors"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

func TestProcessComparisonProviderReadsGroundedJSON(t *testing.T) {
	left, right := id.ID{1}, id.ID{2}
	provider := app.NewProcessComparisonProvider(app.ComparisonProcessProviderConfig{
		Binary: "/bin/sh",
		Args:   []string{"-c", fmt.Sprintf(`grep -q '"source_title":"Notice A"' "$1" && printf '{"status":"completed","text":"Review the selected accounts.","findings":[{"kind":"agreement","summary":"Both notices name the same account.","observation_ids":["%s","%s"]}]}'`, left.String(), right.String()), "comparison-test", app.ComparisonInputPlaceholder},
	})
	found, err := provider.Compare(context.Background(), app.ComparisonInput{Observations: []app.Observation{
		{ID: left, SourceTitle: "Notice A", Statement: "The account @HarborLine posted."},
		{ID: right, SourceTitle: "Notice B", Statement: "@HarborLine was named again."},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if found.Status.String() != "completed" || found.Text == "" || len(found.Findings) != 1 || len(found.Findings[0].ObservationIDs) != 2 {
		t.Fatalf("unexpected comparison: %+v", found)
	}
}

func TestProcessComparisonProviderRejectsUnknownFields(t *testing.T) {
	provider := app.NewProcessComparisonProvider(app.ComparisonProcessProviderConfig{Binary: "/bin/sh", Args: []string{"-c", "printf '%s' \"$1\"", "comparison-test", `{"status":"completed","text":"review","findings":[],"extra":true}`, app.ComparisonInputPlaceholder}, MaxOutput: 1024})
	_, err := provider.Compare(context.Background(), app.ComparisonInput{Observations: []app.Observation{{ID: id.ID{1}, Statement: "source"}}})
	if err == nil {
		t.Fatal("expected provider output to be rejected")
	}
}

func TestProcessComparisonProviderMissingBinaryIsExplicitlyUnavailable(t *testing.T) {
	provider := app.NewProcessComparisonProvider(app.ComparisonProcessProviderConfig{Binary: "not-a-real-overwatch-comparison-provider"})
	_, err := provider.Compare(context.Background(), app.ComparisonInput{Observations: []app.Observation{{ID: id.ID{1}, Statement: "source"}}})
	if !errors.Is(err, app.ErrProcessProviderUnavailable) {
		t.Fatalf("error=%v, want unavailable provider error", err)
	}
	if got := pkgerrors.KindOf(err); got != pkgerrors.Unavailable {
		t.Fatalf("error kind=%s, want unavailable", got)
	}
}
