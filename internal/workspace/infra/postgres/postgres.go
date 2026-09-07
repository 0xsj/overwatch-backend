package postgres

import (
	"context"
	"embed"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/0xsj/overwatch-backend/internal/workspace/domain"
	"github.com/0xsj/overwatch-backend/internal/workspace/infra/postgres/workspacedb"
	"github.com/0xsj/overwatch-backend/pkg/errors"
	"github.com/0xsj/overwatch-backend/pkg/id"
	"github.com/0xsj/overwatch-backend/pkg/postgres"
)

const Schema = "workspace"

//go:embed migrations/*.sql
var migrationFS embed.FS

var Migrations = mustLoad()

func mustLoad() []postgres.Migration {
	ms, err := postgres.FromFS(migrationFS, "migrations")
	if err != nil {
		panic("workspace: " + err.Error())
	}
	return ms
}

type Store struct{ db *postgres.Pool }

func NewStore(db *postgres.Pool) *Store {
	if db == nil {
		panic("workspace: NewStore with a nil Pool")
	}
	return &Store{db: db}
}

func (s *Store) q(ctx context.Context) *workspacedb.Queries {
	return workspacedb.New(s.db.DB(ctx))
}

func uuid(i id.ID) pgtype.UUID { return pgtype.UUID{Bytes: i, Valid: true} }

func maybe(i id.ID) pgtype.UUID {
	if i.IsZero() {
		return pgtype.UUID{}
	}
	return pgtype.UUID{Bytes: i, Valid: true}
}

func ident(u pgtype.UUID) id.ID {
	if !u.Valid {
		return id.Nil
	}
	return id.ID(u.Bytes)
}

func stamp(t time.Time) pgtype.Timestamptz {
	if t.IsZero() {
		return pgtype.Timestamptz{}
	}
	return pgtype.Timestamptz{Time: t, Valid: true}
}

func instant(t pgtype.Timestamptz) time.Time {
	if !t.Valid {
		return time.Time{}
	}
	return t.Time
}

func workspace(row workspacedb.WorkspaceWorkspace) (domain.Workspace, error) {
	status, err := domain.ParseStatus(row.Status)
	if err != nil {
		return domain.Workspace{}, err
	}
	return domain.Workspace{
		ID:          ident(row.ID),
		OrgID:       ident(row.OrgID),
		Name:        row.Name,
		Status:      status,
		Version:     int(row.Version),
		CreatedAt:   instant(row.CreatedAt),
		UpdatedAt:   instant(row.UpdatedAt),
		ArchivedAt:  instant(row.ArchivedAt),
		SourceEvent: ident(row.SourceEventID),
	}, nil
}

func (s *Store) Create(ctx context.Context, w domain.Workspace) error {
	err := s.q(ctx).InsertWorkspace(ctx, workspacedb.InsertWorkspaceParams{
		ID:            uuid(w.ID),
		OrgID:         uuid(w.OrgID),
		Name:          w.Name,
		Status:        w.Status.String(),
		Version:       int32(w.Version),
		CreatedAt:     stamp(w.CreatedAt),
		UpdatedAt:     stamp(w.UpdatedAt),
		ArchivedAt:    stamp(w.ArchivedAt),
		SourceEventID: maybe(w.SourceEvent),
	})
	if err == nil {
		return nil
	}
	translated := postgres.Translate(ctx, err, "workspace: insert")
	if postgres.IsConstraint(translated, "workspace_live_name") {
		return fmt.Errorf("workspace: insert: %w", domain.ErrNameTaken)
	}
	if postgres.IsConstraint(translated, "workspace_source_event") {
		return fmt.Errorf("workspace: insert: %w", domain.ErrAlreadyProvisioned)
	}
	return translated
}

func (s *Store) ByID(ctx context.Context, want id.ID) (domain.Workspace, error) {
	row, err := s.q(ctx).WorkspaceByID(ctx, uuid(want))
	if err != nil {
		translated := postgres.Translate(ctx, err, "workspace: read")
		if errors.IsKind(translated, errors.NotFound) {
			return domain.Workspace{}, fmt.Errorf("workspace: read: %w", domain.ErrNotFound)
		}
		return domain.Workspace{}, translated
	}
	return workspace(row)
}

func (s *Store) ForOrg(ctx context.Context, orgID id.ID) ([]domain.Workspace, error) {
	rows, err := s.q(ctx).WorkspacesForOrg(ctx, uuid(orgID))
	if err != nil {
		return nil, postgres.Translate(ctx, err, "workspace: list")
	}
	out := make([]domain.Workspace, 0, len(rows))
	for _, row := range rows {
		w, err := workspace(row)
		if err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, nil
}

func (s *Store) Save(ctx context.Context, w domain.Workspace) error {
	n, err := s.q(ctx).UpdateWorkspace(ctx, workspacedb.UpdateWorkspaceParams{
		ID:         uuid(w.ID),
		Name:       w.Name,
		Status:     w.Status.String(),
		Version:    int32(w.Version),
		UpdatedAt:  stamp(w.UpdatedAt),
		ArchivedAt: stamp(w.ArchivedAt),
		Version_2:  int32(w.Version - 1),
	})
	if err != nil {
		translated := postgres.Translate(ctx, err, "workspace: update")
		if postgres.IsConstraint(translated, "workspace_live_name") {
			return fmt.Errorf("workspace: update: %w", domain.ErrNameTaken)
		}
		return translated
	}
	if n == 0 {
		if _, err := s.ByID(ctx, w.ID); err != nil {
			return err
		}
		return fmt.Errorf("workspace: update: %w", domain.ErrStaleWrite)
	}
	return nil
}
