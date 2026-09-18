package command_test

import (
	"context"
	"testing"
	"time"

	assistapp "github.com/0xsj/overwatch-backend/internal/assistance/app"
	"github.com/0xsj/overwatch-backend/internal/assistance/app/command"
	"github.com/0xsj/overwatch-backend/internal/assistance/domain"
	"github.com/0xsj/overwatch-backend/pkg/clock"
	"github.com/0xsj/overwatch-backend/pkg/events"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

type synthesisEvidence struct {
	rows map[id.ID]assistapp.Observation
}

func (s synthesisEvidence) Evidence(_ context.Context, _ id.ID, observation id.ID) (assistapp.Observation, error) {
	return s.rows[observation], nil
}

type synthesisRepo struct {
	value  domain.Synthesis
	events int
}

func (s *synthesisRepo) CreateSynthesis(_ context.Context, value domain.Synthesis) error {
	s.value = value
	return nil
}
func (s *synthesisRepo) InTx(ctx context.Context, fn func(context.Context) error) error {
	return fn(ctx)
}
func (s *synthesisRepo) Publish(_ context.Context, evs ...events.Event) error {
	s.events += len(evs)
	return nil
}

func TestGenerateSynthesisPersistsAResumableReviewAid(t *testing.T) {
	at := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)
	ids := id.NewSequence(at)
	workspace, first, second, actor := ids.NewID(), ids.NewID(), ids.NewID(), ids.NewID()
	repo := &synthesisRepo{}
	service := command.NewSyntheses(repo, synthesisEvidence{rows: map[id.ID]assistapp.Observation{
		first:  {ID: first, WorkspaceID: workspace, SourceTitle: "Notice A", Statement: "The account @HarborLine posted.", Quote: "@HarborLine"},
		second: {ID: second, WorkspaceID: workspace, SourceTitle: "Notice B", Statement: "Contact user@example.com.", Quote: "user@example.com"},
	}}, assistapp.LocalSynthesisProvider{}, repo, repo, ids, clock.NewFake(at))
	found, err := service.Generate(context.Background(), workspace, []id.ID{first, second}, actor)
	if err != nil {
		t.Fatal(err)
	}
	if found.ID != repo.value.ID || found.Provider != "local" || found.Method != "selected-observations-v1" || len(found.Candidates) != 2 || repo.events != 1 {
		t.Fatalf("generated synthesis=%+v stored=%+v events=%d", found, repo.value, repo.events)
	}
}
