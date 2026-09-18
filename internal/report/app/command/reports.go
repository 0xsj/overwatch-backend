package command

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/0xsj/overwatch-backend/internal/report/domain"
	"github.com/0xsj/overwatch-backend/pkg/events"
	"github.com/0xsj/overwatch-backend/pkg/id"
	"github.com/0xsj/overwatch-backend/pkg/provenance"
)

// Reports opens a report, toggles its sections, and issues it — decisions/0042.
//
// Opening and toggling are configuration. ISSUING is the act: it renders every
// enabled section, freezes the result as content-addressed bytes, and is the one
// operation here that cannot be undone.
type Reports struct {
	repo      Repository
	sections  Sections
	blobs     Blobs
	tx        Transactor
	publisher events.Publisher
	ids       Minter
	clock     Clock
}

func NewReports(repo Repository, sections Sections, blobs Blobs, tx Transactor,
	publisher events.Publisher, ids Minter, clock Clock) *Reports {
	if repo == nil || sections == nil || blobs == nil || tx == nil ||
		publisher == nil || ids == nil || clock == nil {
		panic("report: NewReports with a nil dependency")
	}
	return &Reports{repo: repo, sections: sections, blobs: blobs, tx: tx,
		publisher: publisher, ids: ids, clock: clock}
}

type Draft struct {
	TargetID    id.ID
	Title       string
	PreparedBy  string
	PeriodStart time.Time
	PeriodEnd   time.Time
}

// Open creates a report and its journal event together, with six sections on.
func (r *Reports) Open(ctx context.Context, workspace, by id.ID, in Draft) (domain.Report, error) {
	now := r.clock.Now()
	fresh, err := domain.New(r.ids.NewID(), workspace, in.TargetID, by,
		in.Title, in.PreparedBy, in.PeriodStart, in.PeriodEnd, now)
	if err != nil {
		return domain.Report{}, err
	}
	err = r.tx.InTx(ctx, func(ctx context.Context) error {
		if err := r.repo.Create(ctx, fresh); err != nil {
			return err
		}
		return r.work(ctx, domain.EventReportOpened, workspace, map[string]string{
			"workspace_id": workspace.String(), "report_id": fresh.ID.String(),
			"target_id": in.TargetID.String(),
		})
	})
	if err != nil {
		return domain.Report{}, err
	}
	return fresh, nil
}

// Toggle turns one section on or off. It refuses to turn off the one that
// cannot be — coverage, and the refusal names why.
func (r *Reports) Toggle(ctx context.Context, workspace, want id.ID,
	section domain.Section, on bool) (domain.Report, error) {
	var next domain.Report
	err := r.tx.InSnapshot(ctx, func(ctx context.Context) error {
		held, err := r.repo.ByID(ctx, workspace, want)
		if err != nil {
			return err
		}
		next, err = held.Toggle(section, on, r.clock.Now())
		if err != nil {
			return err
		}
		return r.repo.Save(ctx, next)
	})
	if err != nil {
		return domain.Report{}, err
	}
	return next, nil
}

// Issue renders the enabled sections and FREEZES them.
//
// Sections share a repeatable-read snapshot. The revision, report counter and
// audit event commit together. Content-addressed blob storage is outside the
// database transaction: a rollback may leave unreferenced bytes, but never a
// visible revision without its audit event.
func (r *Reports) Issue(ctx context.Context, workspace, want, by id.ID) (domain.Revision, error) {
	var revision domain.Revision
	err := r.tx.InSnapshot(ctx, func(ctx context.Context) error {
		held, err := r.repo.ByID(ctx, workspace, want)
		if err != nil {
			return err
		}
		number, err := r.repo.NextRevision(ctx, held.ID)
		if err != nil {
			return err
		}
		now := r.clock.Now()

		doc, err := r.render(ctx, held, number, now)
		if err != nil {
			return err
		}
		// INDENTED, deliberately. These bytes are a deliverable somebody may
		// open in a text editor, and the hash is of what they see rather than
		// of a compressed form only this program can read.
		body, err := json.MarshalIndent(doc, "", "  ")
		if err != nil {
			return fmt.Errorf("report: render: %w", err)
		}
		hash, size, err := r.blobs.Put(ctx, bytes.NewReader(body))
		if err != nil {
			return fmt.Errorf("report: store: %w", err)
		}

		revision, err = domain.NewRevision(r.ids.NewID(), held.ID, workspace, by,
			number, hash, size, held.Enabled(), now)
		if err != nil {
			return err
		}
		if err := r.repo.AddRevision(ctx, revision); err != nil {
			return err
		}
		issued, err := held.Issued(now)
		if err != nil {
			return err
		}
		if err := r.repo.Save(ctx, issued); err != nil {
			return err
		}
		names := make([]string, 0, len(revision.Sections))
		for _, s := range revision.Sections {
			names = append(names, s.String())
		}
		return r.decide(ctx, domain.EventReportIssued, workspace, domain.IssuedEvent{
			WorkspaceID: workspace.String(), ReportID: revision.ReportID.String(),
			Revision: revision.Number, Hash: revision.Hash,
			Sections: names, Withheld: withheldNames(revision.Sections),
		})
	})
	if err != nil {
		return domain.Revision{}, err
	}
	return revision, nil
}

// withheldNames is which capability sections were LEFT OUT. It is computed from
// what was included rather than stored, and it is in the envelope because a
// subscriber cannot compute it — "we sent a report with no scope proof" is the
// sentence an audit reader wants.
func withheldNames(included []domain.Section) []string {
	held := make(map[domain.Section]bool, len(included))
	for _, s := range included {
		held[s] = true
	}
	out := []string{}
	for _, s := range domain.All {
		if s.Withheld() && !held[s] {
			out = append(out, s.String())
		}
	}
	return out
}

// Render is the DRAFT view: exactly what would be frozen, without freezing it.
// It is the same function `Issue` uses, deliberately — two implementations of
// one render drift, and the one that drifts is the preview.
func (r *Reports) Render(ctx context.Context, workspace, want id.ID) (domain.Document, error) {
	var doc domain.Document
	err := r.tx.InSnapshot(ctx, func(ctx context.Context) error {
		held, err := r.repo.ByID(ctx, workspace, want)
		if err != nil {
			return err
		}
		doc, err = r.render(ctx, held, held.Revisions+1, r.clock.Now())
		return err
	})
	if err != nil {
		return domain.Document{}, err
	}
	return doc, nil
}

func (r *Reports) render(ctx context.Context, held domain.Report,
	number int, at time.Time) (domain.Document, error) {
	enabled := held.Enabled()
	doc := domain.Document{
		ReportID: held.ID.String(), WorkspaceID: held.WorkspaceID.String(),
		TargetID: held.TargetID.String(),
		Title:    held.Title, PreparedBy: held.PreparedBy,
		Revision: number,
		Sections: make([]domain.RenderedSection, 0, len(enabled)),
		Withheld: withheldNames(enabled),
	}
	doc.PeriodStart = date(held.PeriodStart)
	doc.PeriodEnd = date(held.PeriodEnd)
	doc.IssuedAt = at.UTC().Format(time.RFC3339)

	for n, section := range enabled {
		one, err := r.section(ctx, held, section)
		if err != nil {
			return domain.Document{}, err
		}
		// THE NUMBER IS DERIVED HERE, over the enabled set. Disable the second
		// section and the rest renumber; a stored number would be wrong the
		// moment a toggle moved.
		one.Number = n + 1
		doc.Sections = append(doc.Sections, one)
	}
	return doc, nil
}

func (r *Reports) section(ctx context.Context, held domain.Report,
	s domain.Section) (domain.RenderedSection, error) {
	out := domain.RenderedSection{Key: s.String(), Title: s.Title()}
	workspace, target := held.WorkspaceID, held.TargetID

	switch s {
	case domain.SectionScope:
		rules, err := r.sections.Scope(ctx, workspace, target)
		if err != nil {
			return out, err
		}
		superseded := 0
		for _, one := range rules {
			if one.Superseded {
				superseded++
			}
		}
		out.Content = rules
		out.Counts = []domain.Count{
			{Label: "rules", Value: len(rules)},
			{Label: "superseded", Value: superseded},
		}

	case domain.SectionAttribution:
		claims, err := r.sections.Attribution(ctx, workspace, target)
		if err != nil {
			return out, err
		}
		by := map[string]int{}
		for _, one := range claims {
			by[one.Claimant]++
		}
		out.Content = claims
		out.Counts = []domain.Count{
			{Label: "claims", Value: len(claims)},
			{Label: "rule", Value: by["rule"]},
			{Label: "human", Value: by["human"]},
			{Label: "model", Value: by["model"]},
		}

	case domain.SectionAssets:
		assets, err := r.sections.Assets(ctx, workspace, target)
		if err != nil {
			return out, err
		}
		inScope := 0
		for _, one := range assets {
			if one.InScope {
				inScope++
			}
		}
		out.Content = assets
		out.Counts = []domain.Count{
			{Label: "assets", Value: len(assets)},
			{Label: "in scope", Value: inScope},
			{Label: "not in scope", Value: len(assets) - inScope},
		}

	case domain.SectionFindings:
		open, dismissed, err := r.sections.Findings(ctx, workspace, target)
		if err != nil {
			return out, err
		}
		out.Content = open
		out.Counts = []domain.Count{
			{Label: "open", Value: len(open)},
			// COUNTED THOUGH EXCLUDED. "1 dismissed and excluded" is one
			// sentence saying both; a silently shorter list says neither.
			{Label: "dismissed and excluded", Value: dismissed},
		}

	case domain.SectionCoverage:
		cells, err := r.sections.Coverage(ctx, workspace, target)
		if err != nil {
			return out, err
		}
		never, stale := 0, 0
		for _, one := range cells {
			switch one.State {
			case "never":
				never++
			case "stale":
				stale++
			}
		}
		out.Content = cells
		// EVERY CELL IS AN APPLICABLE PAIR. `0011` makes inapplicability the
		// absence of a cell rather than a fourth state, so the denominator needs
		// no filtering — and `never` and `stale` are kept APART, because they
		// are different failures: nobody asked, versus the answer is old.
		out.Counts = []domain.Count{
			{Label: "never attempted", Value: never},
			{Label: "stale", Value: stale},
			{Label: "applicable pairs", Value: len(cells)},
		}

	case domain.SectionNotes:
		notes, err := r.sections.Notes(ctx, workspace, target)
		if err != nil {
			return out, err
		}
		authors := map[string]bool{}
		for _, one := range notes {
			authors[one.Author] = true
		}
		out.Content = notes
		out.Counts = []domain.Count{
			{Label: "notes", Value: len(notes)},
			// AUTHORS, because "four notes by one person" and "four notes by
			// four" are different documents and the count alone hides it.
			{Label: "authors", Value: len(authors)},
		}

	case domain.SectionInvocations:
		runs, err := r.sections.Invocations(ctx, workspace, target)
		if err != nil {
			return out, err
		}
		refused, candidates := 0, 0
		for _, one := range runs {
			if one.State == "refused" {
				refused++
			}
			candidates += one.Refused
		}
		out.Content = runs
		out.Counts = []domain.Count{
			{Label: "invocations", Value: len(runs)},
			{Label: "refused before spawn", Value: refused},
			{Label: "candidates refused", Value: candidates},
		}

	case domain.SectionArtifacts:
		artifacts, err := r.sections.Artifacts(ctx, workspace, target)
		if err != nil {
			return out, err
		}
		var total int64
		for _, one := range artifacts {
			total += one.Bytes
		}
		out.Content = artifacts
		out.Counts = []domain.Count{
			{Label: "artifacts", Value: len(artifacts)},
			{Label: "kilobytes", Value: int(total / 1024)},
		}
	}
	return out, nil
}

func date(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format("2006-01-02")
}

func (r *Reports) decide(ctx context.Context, name string, workspace id.ID, payload any) error {
	return r.publish(ctx, name, workspace, payload, true)
}

func (r *Reports) work(ctx context.Context, name string, workspace id.ID, payload any) error {
	return r.publish(ctx, name, workspace, payload, false)
}

func (r *Reports) publish(ctx context.Context, name string, workspace id.ID,
	payload any, decision bool) error {
	prov, ok := provenance.Current(ctx)
	if !ok {
		prov = provenance.New(provenance.OriginRequest, r.ids)
	}
	tenanted, err := prov.WithTenant(workspace.String())
	if err != nil {
		return fmt.Errorf("report: %s: %w", name, err)
	}
	subject := domain.SubjectKind + ":" + workspace.String()
	var e events.Event
	if decision {
		e, err = events.NewDecision(r.ids, r.clock, name, subject, tenanted, payload)
	} else {
		e, err = events.New(r.ids, r.clock, name, subject, tenanted, payload)
	}
	if err != nil {
		return fmt.Errorf("report: %s: %w", name, err)
	}
	if err := r.publisher.Publish(ctx, e); err != nil {
		return fmt.Errorf("report: %s: %w", name, err)
	}
	return nil
}
