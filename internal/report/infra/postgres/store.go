package postgres

import (
	"context"

	"github.com/0xsj/overwatch-backend/internal/report/domain"
	"github.com/0xsj/overwatch-backend/internal/report/infra/postgres/reportdb"
	"github.com/0xsj/overwatch-backend/pkg/id"
	"github.com/0xsj/overwatch-backend/pkg/postgres"
)

// Create writes the report AND its section rows. The two are one act: a report
// with no section rows would read as every section defaulted, which is true
// today and stops being true the moment a default moves — and that is exactly
// what storing the set is for.
func (s *Store) Create(ctx context.Context, r domain.Report) error {
	err := s.q(ctx).InsertReport(ctx, reportdb.InsertReportParams{
		ID: uuid(r.ID), WorkspaceID: uuid(r.WorkspaceID), TargetID: uuid(r.TargetID),
		Title: r.Title, PreparedBy: text(r.PreparedBy),
		PeriodStart: day(r.PeriodStart), PeriodEnd: day(r.PeriodEnd),
		Revisions: int32(r.Revisions), CreatedBy: uuid(r.CreatedBy),
		CreatedAt: stamp(r.CreatedAt), UpdatedAt: stamp(r.UpdatedAt),
	})
	if err != nil {
		return postgres.Translate(ctx, err, "report: insert")
	}
	return s.writeSections(ctx, r)
}

func (s *Store) writeSections(ctx context.Context, r domain.Report) error {
	for _, section := range domain.All {
		err := s.q(ctx).SetSection(ctx, reportdb.SetSectionParams{
			ReportID: uuid(r.ID), Section: section.String(), Enabled: r.On(section),
		})
		if err != nil {
			return postgres.Translate(ctx, err, "report: set section")
		}
	}
	return nil
}

func (s *Store) ByID(ctx context.Context, workspace, want id.ID) (domain.Report, error) {
	row, err := s.q(ctx).ReportByID(ctx, reportdb.ReportByIDParams{
		ID: uuid(want), WorkspaceID: uuid(workspace),
	})
	if err != nil {
		return domain.Report{}, notFound(ctx, err, "report: read", domain.ErrNotFound)
	}
	out := report(row)
	sections, err := s.q(ctx).SectionsForReport(ctx, uuid(want))
	if err != nil {
		return domain.Report{}, postgres.Translate(ctx, err, "report: sections")
	}
	for _, one := range sections {
		section, err := domain.ParseSection(one.Section)
		if err != nil {
			// A row this build no longer knows is SKIPPED, not fatal — unlike a
			// revision's list. A configuration naming a retired section is a
			// stale toggle; a revision naming one is a false claim about what
			// somebody received.
			continue
		}
		out.Sections[section] = one.Enabled
	}
	return out, nil
}

func (s *Store) Page(ctx context.Context, workspace, target id.ID, limit int) ([]domain.Report, error) {
	rows, err := s.q(ctx).ReportsForTarget(ctx, reportdb.ReportsForTargetParams{
		WorkspaceID: uuid(workspace), Target: maybe(target), Page: int32(limit),
	})
	if err != nil {
		return nil, postgres.Translate(ctx, err, "report: page")
	}
	out := make([]domain.Report, 0, len(rows))
	for _, row := range rows {
		out = append(out, report(row))
	}
	return out, nil
}

// Save writes the configuration and its sections together, for the same reason
// Create does.
func (s *Store) Save(ctx context.Context, r domain.Report) error {
	n, err := s.q(ctx).SaveReport(ctx, reportdb.SaveReportParams{
		ID: uuid(r.ID), WorkspaceID: uuid(r.WorkspaceID),
		Title: r.Title, PreparedBy: text(r.PreparedBy),
		PeriodStart: day(r.PeriodStart), PeriodEnd: day(r.PeriodEnd),
		Revisions: int32(r.Revisions), UpdatedAt: stamp(r.UpdatedAt),
	})
	if err != nil {
		return postgres.Translate(ctx, err, "report: save")
	}
	if n == 0 {
		return domain.ErrNotFound
	}
	return s.writeSections(ctx, r)
}

func (s *Store) AddRevision(ctx context.Context, rev domain.Revision) error {
	names := make([]string, 0, len(rev.Sections))
	for _, one := range rev.Sections {
		names = append(names, one.String())
	}
	err := s.q(ctx).InsertRevision(ctx, reportdb.InsertRevisionParams{
		ID: uuid(rev.ID), ReportID: uuid(rev.ReportID),
		WorkspaceID: uuid(rev.WorkspaceID), Number: int32(rev.Number),
		Hash: rev.Hash, Bytes: rev.Bytes, MediaType: rev.MediaType,
		Sections: names, IssuedBy: uuid(rev.IssuedBy), IssuedAt: stamp(rev.IssuedAt),
	})
	if err != nil {
		return postgres.Translate(ctx, err, "report: insert revision")
	}
	return nil
}

func (s *Store) NextRevision(ctx context.Context, report id.ID) (int, error) {
	n, err := s.q(ctx).NextRevisionNumber(ctx, uuid(report))
	if err != nil {
		return 0, postgres.Translate(ctx, err, "report: next revision")
	}
	return int(n), nil
}

// Revisions is newest FIRST. A reader opening a report wants what was last sent,
// and revision 1 is history.
func (s *Store) Revisions(ctx context.Context, report id.ID) ([]domain.Revision, error) {
	rows, err := s.q(ctx).RevisionsForReport(ctx, uuid(report))
	if err != nil {
		return nil, postgres.Translate(ctx, err, "report: revisions")
	}
	out := make([]domain.Revision, 0, len(rows))
	for _, row := range rows {
		one, err := revision(row)
		if err != nil {
			return nil, err
		}
		out = append(out, one)
	}
	return out, nil
}

// RevisionByID reads by WORKSPACE and not through its report, because a client
// reaching a delivered document must not need to read the configuration that
// produced it.
func (s *Store) RevisionByID(ctx context.Context, workspace, want id.ID) (domain.Revision, error) {
	row, err := s.q(ctx).RevisionByID(ctx, reportdb.RevisionByIDParams{
		ID: uuid(want), WorkspaceID: uuid(workspace),
	})
	if err != nil {
		return domain.Revision{}, notFound(ctx, err, "report: read revision", domain.ErrRevisionNotFound)
	}
	return revision(row)
}
