package query

import (
	"context"
	"strings"

	"github.com/0xsj/overwatch-backend/internal/researchentity/domain"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

type Reader interface {
	Page(context.Context, id.ID, id.ID, string, domain.Kind, domain.CitationFilter, domain.ResolutionFilter, domain.ArchiveFilter, int, string) ([]domain.Record, error)
	Summary(context.Context, id.ID, string) (domain.BrowseSummary, error)
	ByIDVisible(context.Context, id.ID, id.ID, string) (domain.Record, error)
	Revisions(context.Context, id.ID, id.ID, string) ([]domain.Revision, error)
}

type Records struct{ reader Reader }

func NewRecords(reader Reader) *Records {
	if reader == nil {
		panic("researchentity: NewRecords with a nil reader")
	}
	return &Records{reader: reader}
}

const (
	DefaultPage = 50
	MaxPage     = 100
)

type Page struct {
	Items      []domain.Record `json:"items"`
	NextCursor *id.ID          `json:"next_cursor"`
}

func (r *Records) List(ctx context.Context, workspace, before id.ID, search string, kind domain.Kind, citation domain.CitationFilter, resolution domain.ResolutionFilter, archived domain.ArchiveFilter, limit int, maxSensitivity string) (Page, error) {
	if workspace.IsZero() || !validMaxSensitivity(maxSensitivity) {
		return Page{}, domain.ErrWorkspaceRequired
	}
	search = strings.TrimSpace(search)
	if len(search) > 200 {
		return Page{}, domain.ErrSearchTooLong
	}
	if kind != "" {
		parsed, err := domain.ParseKind(kind.String())
		if err != nil {
			return Page{}, err
		}
		kind = parsed
	}
	parsedCitation, err := domain.ParseCitationFilter(citation.String())
	if err != nil {
		return Page{}, err
	}
	parsedResolution, err := domain.ParseResolutionFilter(resolution.String())
	if err != nil {
		return Page{}, err
	}
	parsedArchived, err := domain.ParseArchiveFilter(archived.String())
	if err != nil {
		return Page{}, err
	}
	if limit <= 0 {
		limit = DefaultPage
	}
	if limit > MaxPage {
		limit = MaxPage
	}
	rows, err := r.reader.Page(ctx, workspace, before, search, kind, parsedCitation, parsedResolution, parsedArchived, limit+1, maxSensitivity)
	if err != nil {
		return Page{}, err
	}
	out := Page{Items: rows}
	if out.Items == nil {
		out.Items = []domain.Record{}
	}
	if len(rows) > limit {
		cursor := rows[limit-1].ID
		out.NextCursor = &cursor
		out.Items = rows[:limit]
	}
	return out, nil
}

func (r *Records) ByID(ctx context.Context, workspace, want id.ID, maxSensitivity string) (domain.Record, error) {
	if workspace.IsZero() || want.IsZero() || !validMaxSensitivity(maxSensitivity) {
		return domain.Record{}, domain.ErrIDRequired
	}
	return r.reader.ByIDVisible(ctx, workspace, want, maxSensitivity)
}

func (r *Records) Summary(ctx context.Context, workspace id.ID, maxSensitivity string) (domain.BrowseSummary, error) {
	if workspace.IsZero() || !validMaxSensitivity(maxSensitivity) {
		return domain.BrowseSummary{}, domain.ErrWorkspaceRequired
	}
	return r.reader.Summary(ctx, workspace, maxSensitivity)
}

func (r *Records) Revisions(ctx context.Context, workspace, record id.ID, maxSensitivity string) ([]domain.Revision, error) {
	if workspace.IsZero() || record.IsZero() || !validMaxSensitivity(maxSensitivity) {
		return nil, domain.ErrIDRequired
	}
	rows, err := r.reader.Revisions(ctx, workspace, record, maxSensitivity)
	if err != nil {
		return nil, err
	}
	if rows == nil {
		return []domain.Revision{}, nil
	}
	return rows, nil
}

func validMaxSensitivity(value string) bool {
	return value == "internal" || value == "restricted"
}
