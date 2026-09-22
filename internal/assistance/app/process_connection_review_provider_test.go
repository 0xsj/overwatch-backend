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

func TestProcessConnectionReviewProviderReadsGroundedJSON(t *testing.T) {
	connection, supporting, opposing := id.ID{1}, id.ID{2}, id.ID{3}
	provider := app.NewProcessConnectionReviewProvider(app.ConnectionReviewProcessProviderConfig{
		Binary: "/bin/sh",
		Args:   []string{"-c", fmt.Sprintf(`grep -q '"kind":"associated_with"' "$1" && printf '{"status":"completed","text":"Reviewable connection findings.","findings":[{"kind":"support","summary":"The selected citation supports review of the authored relationship.","observation_ids":["%s"]},{"kind":"opposition","summary":"The opposing citation should remain visible during review.","observation_ids":["%s"]}]}'`, supporting.String(), opposing.String()), "connection-test", app.ConnectionReviewInputPlaceholder},
	})
	result, err := provider.ReviewConnection(context.Background(), app.ConnectionReviewInput{
		ConnectionID: connection, FromRecordID: id.ID{4}, ToRecordID: id.ID{5}, ConnectionKind: "associated_with", ConnectionState: "proposed", ConnectionRationale: "review the authored relationship",
		SupportingObservationIDs: []id.ID{supporting}, OpposingObservationIDs: []id.ID{opposing},
		SupportingObservations: []app.Observation{{ID: supporting, SourceTitle: "Notice A", Statement: "The subject appeared at the location."}},
		OpposingObservations:   []app.Observation{{ID: opposing, SourceTitle: "Notice B", Statement: "The account disputes the association."}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != domain.ConnectionReviewCompleted || result.Text == "" || len(result.Findings) != 2 || result.Findings[0].ObservationIDs[0] != supporting || result.Findings[1].Kind != domain.ConnectionOpposition {
		t.Fatalf("unexpected connection review: %+v", result)
	}
}

func TestProcessConnectionReviewProviderRejectsUnknownFields(t *testing.T) {
	provider := app.NewProcessConnectionReviewProvider(app.ConnectionReviewProcessProviderConfig{Binary: "/bin/sh", Args: []string{"-c", "printf '%s' \"$1\"", "connection-test", `{"status":"completed","text":"review","findings":[],"extra":true}`, app.ConnectionReviewInputPlaceholder}, MaxOutput: 1024})
	_, err := provider.ReviewConnection(context.Background(), app.ConnectionReviewInput{ConnectionID: id.ID{1}, FromRecordID: id.ID{2}, ToRecordID: id.ID{3}, ConnectionKind: "associated_with", ConnectionState: "proposed", ConnectionRationale: "review", SupportingObservationIDs: []id.ID{{4}}})
	if err == nil {
		t.Fatal("expected provider output to be rejected")
	}
}

func TestProcessConnectionReviewProviderMissingBinaryIsExplicitlyUnavailable(t *testing.T) {
	provider := app.NewProcessConnectionReviewProvider(app.ConnectionReviewProcessProviderConfig{Binary: "not-a-real-overwatch-connection-review-provider"})
	_, err := provider.ReviewConnection(context.Background(), app.ConnectionReviewInput{ConnectionID: id.ID{1}, FromRecordID: id.ID{2}, ToRecordID: id.ID{3}, ConnectionKind: "associated_with", ConnectionState: "proposed", ConnectionRationale: "review", SupportingObservationIDs: []id.ID{{4}}})
	if !errors.Is(err, app.ErrProcessProviderUnavailable) {
		t.Fatalf("error=%v, want unavailable provider error", err)
	}
	if got := pkgerrors.KindOf(err); got != pkgerrors.Unavailable {
		t.Fatalf("error kind=%s, want unavailable", got)
	}
}
