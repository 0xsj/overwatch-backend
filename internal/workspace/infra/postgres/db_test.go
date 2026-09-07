package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/0xsj/overwatch-backend/internal/workspace/domain"
	workspacepg "github.com/0xsj/overwatch-backend/internal/workspace/infra/postgres"
	"github.com/0xsj/overwatch-backend/pkg/errors"
	"github.com/0xsj/overwatch-backend/pkg/id"
	"github.com/0xsj/overwatch-backend/pkg/testx"
)

var at = time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)

func store(t *testing.T) *workspacepg.Store {
	t.Helper()
	p := testx.Postgres(t, testx.Schema{Name: workspacepg.Schema, Migrations: workspacepg.Migrations})
	return workspacepg.NewStore(p)
}

func ids() *id.Sequence { return id.NewSequence(at) }

func made(t *testing.T, s *workspacepg.Store, m *id.Sequence, org id.ID, name string) domain.Workspace {
	t.Helper()
	w, err := domain.New(m.NewID(), org, name, at)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Create(context.Background(), w); err != nil {
		t.Fatalf("create %q: %v", name, err)
	}
	return w
}

func TestASoloHunterKeepsSeveralWorkspacesApart(t *testing.T) {
	s := store(t)
	ctx := context.Background()
	m := ids()
	org := m.NewID()
	other := m.NewID()

	made(t, s, m, org, "CTF1")
	made(t, s, m, org, "CTF2")
	made(t, s, m, other, "Someone else's work")

	mine, err := s.ForOrg(ctx, org)
	if err != nil {
		t.Fatal(err)
	}
	if len(mine) != 2 {
		t.Fatalf("the org has %d workspaces, want 2 and nobody else's", len(mine))
	}
}

// PARTIAL, and the predicate is the decision. Two live "Acme Q3" inside one org
// is a mistake nobody can see on a switcher; a total index would burn the name
// for good the first time an engagement closed.
func TestANameIsUniqueWhileLiveAndIsReleasedOnArchive(t *testing.T) {
	s := store(t)
	ctx := context.Background()
	m := ids()
	org := m.NewID()
	first := made(t, s, m, org, "Acme Q3")

	dup, err := domain.New(m.NewID(), org, "Acme Q3", at)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Create(ctx, dup); !errors.Is(err, domain.ErrNameTaken) {
		t.Fatalf("a second live workspace of that name gave %v, want ErrNameTaken", err)
	}

	// Case-insensitively, because a switcher showing "Acme Q3" and "acme q3" is
	// the same mistake with extra steps.
	cased, err := domain.New(m.NewID(), org, "acme q3", at)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Create(ctx, cased); !errors.Is(err, domain.ErrNameTaken) {
		t.Fatalf("a case variant gave %v, want ErrNameTaken", err)
	}

	// A different org may use it freely — that is the wall.
	if err := s.Create(ctx, mustNew(t, m, m.NewID(), "Acme Q3")); err != nil {
		t.Fatalf("another org was blocked by this org's name: %v", err)
	}

	archived, err := first.Archive(at.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Save(ctx, archived); err != nil {
		t.Fatal(err)
	}
	if err := s.Create(ctx, dup); err != nil {
		t.Errorf("the name was not released on archive: %v", err)
	}
}

func TestArchivedIsTerminalAndTheRowSurvives(t *testing.T) {
	s := store(t)
	ctx := context.Background()
	m := ids()
	org := m.NewID()
	w := made(t, s, m, org, "Closed engagement")

	archived, err := w.Archive(at.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Save(ctx, archived); err != nil {
		t.Fatal(err)
	}

	// Gone from the list a switcher renders...
	live, err := s.ForOrg(ctx, org)
	if err != nil {
		t.Fatal(err)
	}
	if len(live) != 0 {
		t.Errorf("an archived workspace is still listed: %+v", live)
	}
	// ...and still readable by id, because every target and audit entry names it.
	back, err := s.ByID(ctx, w.ID)
	if err != nil {
		t.Fatalf("the archived row is gone: %v", err)
	}
	if !back.Archived() || back.ArchivedAt.IsZero() {
		t.Errorf("status and archived_at disagree: %+v", back)
	}
	if _, err := back.Archive(at.Add(2 * time.Hour)); !errors.Is(err, domain.ErrArchived) {
		t.Error("archiving twice was allowed")
	}
	if _, err := back.Rename("New name", at.Add(2*time.Hour)); !errors.Is(err, domain.ErrArchived) {
		t.Error("an archived workspace was renamed")
	}
}

func TestAStaleSaveIsRefusedRatherThanLost(t *testing.T) {
	s := store(t)
	ctx := context.Background()
	m := ids()
	w := made(t, s, m, m.NewID(), "CTF1")

	mine, _ := w.Rename("Mine", at.Add(time.Minute))
	theirs, _ := w.Rename("Theirs", at.Add(time.Minute))
	if err := s.Save(ctx, mine); err != nil {
		t.Fatal(err)
	}
	if err := s.Save(ctx, theirs); !errors.Is(err, domain.ErrStaleWrite) {
		t.Fatalf("the losing write gave %v, want ErrStaleWrite", err)
	}
	back, _ := s.ByID(ctx, w.ID)
	if back.Name != "Mine" {
		t.Errorf("name is %q — the losing write was applied", back.Name)
	}
}

// org_id names a row in org's schema and is deliberately not a reference. The
// cost, asserted rather than assumed.
func TestAWorkspaceNamingANonexistentOrgIsAcceptedAndThatIsTheTrade(t *testing.T) {
	s := store(t)
	m := ids()
	if err := s.Create(context.Background(), mustNew(t, m, m.NewID(), "Orphan")); err != nil {
		t.Fatalf("no foreign key crosses out of this schema, so this must succeed: %v", err)
	}
}

func mustNew(t *testing.T, m *id.Sequence, org id.ID, name string) domain.Workspace {
	t.Helper()
	w, err := domain.New(m.NewID(), org, name, at)
	if err != nil {
		t.Fatal(err)
	}
	return w
}
