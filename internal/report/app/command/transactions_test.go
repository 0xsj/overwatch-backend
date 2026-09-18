package command_test

import (
	"context"
	"errors"
	"testing"

	"github.com/0xsj/overwatch-backend/internal/report/app/command"
	"github.com/0xsj/overwatch-backend/internal/report/domain"
	"github.com/0xsj/overwatch-backend/pkg/events"
)

type transactionMarker struct{}

// This models the repository and outbox sharing a transaction. Publishing with
// the request context instead of the transaction context is explicitly refused.
type reportTransaction struct {
	repo      *store
	published []events.Event
	fail      error
	snapshots int
}

func (s *reportTransaction) InTx(ctx context.Context, fn func(context.Context) error) error {
	before, revisions, published := s.repo.held, len(s.repo.revisions), len(s.published)
	err := fn(context.WithValue(ctx, transactionMarker{}, true))
	if err != nil {
		s.repo.held = before
		s.repo.revisions = s.repo.revisions[:revisions]
		s.published = s.published[:published]
	}
	return err
}

func (s *reportTransaction) InSnapshot(ctx context.Context, fn func(context.Context) error) error {
	s.snapshots++
	return s.InTx(ctx, fn)
}

func (s *reportTransaction) Publish(ctx context.Context, evs ...events.Event) error {
	if ctx.Value(transactionMarker{}) != true {
		return errors.New("event published outside the business transaction")
	}
	if s.fail != nil {
		return s.fail
	}
	s.published = append(s.published, evs...)
	return nil
}

func TestOpenRollsBackTheReportWhenItsEventCannotBeWritten(t *testing.T) {
	ctx := context.Background()
	s := &reportTransaction{repo: &store{}, fail: errors.New("outbox unavailable")}
	r := command.NewReports(s.repo, said{}, &keptBytes{}, s, s, &minter{}, frozen{})
	got, err := r.Open(ctx, an(1), an(2), command.Draft{TargetID: an(3), Title: "Review"})
	if !errors.Is(err, s.fail) || !got.ID.IsZero() || !s.repo.held.ID.IsZero() {
		t.Fatalf("failed publication must leave no report: result=%+v stored=%+v err=%v", got, s.repo.held, err)
	}
}

func TestIssueRollsBackRevisionAndCounterWhenAuditPublicationFails(t *testing.T) {
	ctx := context.Background()
	s := &reportTransaction{repo: &store{}}
	r := command.NewReports(s.repo, said{}, &keptBytes{}, s, s, &minter{}, frozen{})
	opened, err := r.Open(ctx, an(1), an(2), command.Draft{TargetID: an(3), Title: "Review"})
	if err != nil {
		t.Fatal(err)
	}
	s.fail = errors.New("outbox unavailable")
	got, err := r.Issue(ctx, an(1), opened.ID, an(2))
	if !errors.Is(err, s.fail) || !got.ID.IsZero() || len(s.repo.revisions) != 0 || s.repo.held.Revisions != 0 {
		t.Fatalf("failed issue left a revision or counter: result=%+v stored=%+v err=%v", got, s.repo, err)
	}
	if len(s.published) != 1 || s.snapshots != 1 {
		t.Fatalf("only opening should be published, issue requires a snapshot: %+v", s)
	}

	// The failed attempt must not consume a revision number or leave a phantom
	// audit event. Retrying produces revision one and exactly one issue event.
	s.fail = nil
	got, err = r.Issue(ctx, an(1), opened.ID, an(2))
	if err != nil || got.Number != 1 || s.repo.held.Revisions != 1 || len(s.repo.revisions) != 1 {
		t.Fatalf("retry: result=%+v stored=%+v err=%v", got, s.repo, err)
	}
	if len(s.published) != 2 || s.published[1].Name != domain.EventReportIssued || !s.published[1].Decision {
		t.Fatalf("retry must publish one audit decision: %+v", s.published)
	}
}

func TestPreviewAndSectionChangesUseTheSnapshotBoundary(t *testing.T) {
	ctx := context.Background()
	s := &reportTransaction{repo: &store{}}
	r := command.NewReports(s.repo, said{}, &keptBytes{}, s, s, &minter{}, frozen{})
	opened, err := r.Open(ctx, an(1), an(2), command.Draft{TargetID: an(3), Title: "Review"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.Toggle(ctx, an(1), opened.ID, domain.SectionArtifacts, true); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Render(ctx, an(1), opened.ID); err != nil {
		t.Fatal(err)
	}
	if s.snapshots != 2 || len(s.repo.revisions) != 0 {
		t.Fatalf("preview/toggle need snapshots without issuing: %+v", s)
	}
}
