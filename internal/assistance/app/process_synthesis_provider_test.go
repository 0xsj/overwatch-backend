package app_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/0xsj/overwatch-backend/internal/assistance/app"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

func TestProcessSynthesisProviderPassesOnlySelectedObservationsAndReadsGroundedJSON(t *testing.T) {
	observation := id.ID{1}
	provider := app.NewProcessSynthesisProvider(app.SynthesisProcessProviderConfig{
		Binary: "/bin/sh",
		Args:   []string{"-c", fmt.Sprintf(`grep -q '"source_title":"Notice A"' "$1" && printf '{"text":"The selected notice names an account.","candidates":[{"kind":"account","name":"@HarborLine","observation_ids":["%s"],"rationale":"Verify the exact handle before creating a record."}]}'`, observation.String()), "synthesis-test", app.SynthesisInputPlaceholder},
	})
	found, err := provider.Synthesize(context.Background(), []app.Observation{{
		ID: observation, SourceTitle: "Notice A", Statement: "The account @HarborLine posted.", Quote: "@HarborLine",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if found.Text == "" || len(found.Candidates) != 1 || found.Candidates[0].Name != "@HarborLine" || found.Candidates[0].ObservationIDs[0] != observation {
		t.Fatalf("unexpected synthesis: %+v", found)
	}
}

func TestProcessSynthesisProviderRejectsUngroundedCandidatesAndUnknownFields(t *testing.T) {
	observation := id.ID{1}
	for name, output := range map[string]string{
		"ungrounded":    fmt.Sprintf(`{"text":"review","candidates":[{"kind":"account","name":"@invented","observation_ids":["%s"],"rationale":"hallucinated"}]}`, observation.String()),
		"unknown field": `{"text":"review","candidates":[],"extra":"reject"}`,
	} {
		t.Run(name, func(t *testing.T) {
			provider := app.NewProcessSynthesisProvider(app.SynthesisProcessProviderConfig{Binary: "/bin/sh", Args: []string{"-c", "printf '%s' \"$1\"", "synthesis-test", output, app.SynthesisInputPlaceholder}, MaxOutput: 1024})
			_, err := provider.Synthesize(context.Background(), []app.Observation{{ID: observation, Statement: "The account @HarborLine posted."}})
			if err == nil {
				t.Fatal("expected provider output to be rejected")
			}
		})
	}
}

func TestProcessSynthesisProviderMissingBinaryIsExplicitlyUnavailable(t *testing.T) {
	provider := app.NewProcessSynthesisProvider(app.SynthesisProcessProviderConfig{Binary: "not-a-real-overwatch-synthesis-provider"})
	_, err := provider.Synthesize(context.Background(), []app.Observation{{ID: id.ID{1}, Statement: "source"}})
	if !errors.Is(err, app.ErrSynthesisProviderUnavailable) {
		t.Fatalf("error=%v, want ErrSynthesisProviderUnavailable", err)
	}
}
