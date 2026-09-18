package command

import (
	"context"
	"fmt"
	"time"

	"github.com/0xsj/overwatch-backend/internal/note/domain"
	"github.com/0xsj/overwatch-backend/pkg/events"
	"github.com/0xsj/overwatch-backend/pkg/id"
	"github.com/0xsj/overwatch-backend/pkg/provenance"
)

type Repository interface {
	Create(ctx context.Context, n domain.Note) error
	ByID(ctx context.Context, workspace, want id.ID) (domain.Note, error)
	Save(ctx context.Context, n domain.Note) error
	Delete(ctx context.Context, workspace, want id.ID) error
}

// Kinds is the port into `scope`, which holds the canonical kind vocabulary —
// `0034`. `note` may not import it, so the composition root asks.
//
// **It is a port rather than a copied list**, because a sixth copy of fourteen
// words is a sixth thing to drift. `root/vocabulary_test.go` enforces the three
// that ARE copied; this one avoids needing to be enforced.
type Kinds interface {
	Known(kind string) bool
}

type Minter interface{ NewID() id.ID }

type Clock interface{ Now() time.Time }

// Notes is the only command in this system whose input comes from a person and
// nowhere else.
type Notes struct {
	repo      Repository
	kinds     Kinds
	publisher events.Publisher
	ids       Minter
	clock     Clock
}

func NewNotes(repo Repository, kinds Kinds, publisher events.Publisher,
	ids Minter, clock Clock) *Notes {
	if repo == nil || kinds == nil || publisher == nil || ids == nil || clock == nil {
		panic("note: NewNotes with a nil dependency")
	}
	return &Notes{repo: repo, kinds: kinds, publisher: publisher, ids: ids, clock: clock}
}

// Write records a note. A SUBJECTLESS one is the engagement summary — `0042`'s
// eighth report section — and is the same row shape.
func (n *Notes) Write(ctx context.Context, workspace, author id.ID,
	subjectKind, subjectValue, body string) (domain.Note, error) {
	// **THE KIND IS CHECKED BEFORE THE DOMAIN SEES IT.** A note about
	// `hosst:acme.test` would attach to nothing and read as a note about
	// something — the quiet failure this vocabulary exists to prevent.
	if subjectKind != "" && !n.kinds.Known(subjectKind) {
		return domain.Note{}, domain.ErrKindUnknown
	}
	fresh, err := domain.New(n.ids.NewID(), workspace, author,
		subjectKind, subjectValue, body, n.clock.Now())
	if err != nil {
		return domain.Note{}, err
	}
	if err := n.repo.Create(ctx, fresh); err != nil {
		return domain.Note{}, err
	}
	return fresh, n.decide(ctx, workspace, fresh, false)
}

// Edit replaces the body. Only the author may — see [domain.Note.Edit].
func (n *Notes) Edit(ctx context.Context, workspace, want, by id.ID, body string) (domain.Note, error) {
	held, err := n.repo.ByID(ctx, workspace, want)
	if err != nil {
		return domain.Note{}, err
	}
	next, err := held.Edit(by, body, n.clock.Now())
	if err != nil {
		return domain.Note{}, err
	}
	if err := n.repo.Save(ctx, next); err != nil {
		return domain.Note{}, err
	}
	return next, n.decide(ctx, workspace, next, true)
}

// Erase removes a note. **Only the author**, for the same reason only the author
// may edit: somebody else deleting a person's statement is the same act with a
// bigger hammer.
//
// It is a real delete and not an archive. A note is working text with no
// citation pointing at it — `0043` §2 — and the one place its words are
// load-bearing is a frozen report, which holds the bytes and does not care that
// the row is gone.
func (n *Notes) Erase(ctx context.Context, workspace, want, by id.ID) error {
	held, err := n.repo.ByID(ctx, workspace, want)
	if err != nil {
		return err
	}
	if held.Author != by {
		return domain.ErrNotYours
	}
	return n.repo.Delete(ctx, workspace, want)
}

// decide publishes a DECISION — 0014. A person chose to record something about a
// client's estate: that has an author, no outcome column, and a reason to
// outlive the journal's retention.
func (n *Notes) decide(ctx context.Context, workspace id.ID, of domain.Note, edit bool) error {
	prov, ok := provenance.Current(ctx)
	if !ok {
		prov = provenance.New(provenance.OriginRequest, n.ids)
	}
	tenanted, err := prov.WithTenant(workspace.String())
	if err != nil {
		return fmt.Errorf("note: %s: %w", domain.EventNoteWritten, err)
	}
	about := ""
	if of.About() {
		about = of.SubjectKind + ":" + of.SubjectValue
	}
	e, err := events.NewDecision(n.ids, n.clock, domain.EventNoteWritten,
		domain.SubjectKind+":"+workspace.String(), tenanted, domain.Written{
			WorkspaceID: workspace.String(), NoteID: of.ID.String(),
			About: about, Edit: edit,
		})
	if err != nil {
		return fmt.Errorf("note: %s: %w", domain.EventNoteWritten, err)
	}
	if err := n.publisher.Publish(ctx, e); err != nil {
		return fmt.Errorf("note: %s: %w", domain.EventNoteWritten, err)
	}
	return nil
}
