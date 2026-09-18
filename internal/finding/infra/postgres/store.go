package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/0xsj/overwatch-backend/internal/finding/domain"
	"github.com/0xsj/overwatch-backend/internal/finding/infra/postgres/findingdb"
	"github.com/0xsj/overwatch-backend/pkg/errors"
	"github.com/0xsj/overwatch-backend/pkg/id"
	"github.com/0xsj/overwatch-backend/pkg/postgres"
)

// Record is the upsert — decisions/0041 §1. It answers the stored finding and
// whether this call OPENED it, because "a new problem" and "we saw it again" are
// two different events and a second read to tell them apart would be a read per
// finding per scan.
func (s *Store) Record(ctx context.Context, f domain.Finding) (domain.Finding, bool, error) {
	row, err := s.q(ctx).UpsertFinding(ctx, findingdb.UpsertFindingParams{
		ID: uuid(f.ID), WorkspaceID: uuid(f.WorkspaceID), ToolID: uuid(f.ToolID),
		Signature: f.Signature, FragmentID: uuid(f.FragmentID),
		FragmentKind: f.FragmentKind, FragmentValue: f.FragmentValue,
		State: f.State.String(), Severity: f.Severity.String(),
		Claimant: f.Assessment.Claimant.String(), Actor: maybe(f.Assessment.Actor),
		Confidence: confidence(f.Assessment.Confidence, f.Assessment.HasConfidence),
		Basis:      text(f.Assessment.Basis), AssessedAt: stamp(f.Assessment.At),
		FirstSeen: stamp(f.FirstSeen), LastSeen: stamp(f.LastSeen),
		Sightings:    int32(f.Sightings),
		InvocationID: uuid(f.Invocation), ArtifactID: uuid(f.Artifact),
		MappingID: uuid(f.MappingID), CreatedAt: stamp(f.CreatedAt),
	})
	if err != nil {
		return domain.Finding{}, false, postgres.Translate(ctx, err, "finding: record")
	}
	got, err := finding(findingdb.FindingFinding{
		ID: row.ID, WorkspaceID: row.WorkspaceID, ToolID: row.ToolID,
		Signature: row.Signature, FragmentID: row.FragmentID,
		FragmentKind: row.FragmentKind, FragmentValue: row.FragmentValue,
		State: row.State, Reason: row.Reason, DecidedBy: row.DecidedBy,
		DecidedAt: row.DecidedAt, Severity: row.Severity, Claimant: row.Claimant,
		Actor: row.Actor, Confidence: row.Confidence, Basis: row.Basis,
		AssessedAt:           row.AssessedAt,
		SupersededSeverity:   row.SupersededSeverity,
		SupersededClaimant:   row.SupersededClaimant,
		SupersededActor:      row.SupersededActor,
		SupersededConfidence: row.SupersededConfidence,
		SupersededBasis:      row.SupersededBasis,
		SupersededAt:         row.SupersededAt,
		FirstSeen:            row.FirstSeen, LastSeen: row.LastSeen,
		Sightings: row.Sightings, InvocationID: row.InvocationID,
		ArtifactID: row.ArtifactID, MappingID: row.MappingID,
		CreatedAt: row.CreatedAt,
	})
	return got, row.Opened, err
}

func (s *Store) ByID(ctx context.Context, workspace, want id.ID) (domain.Finding, error) {
	row, err := s.q(ctx).FindingByID(ctx, findingdb.FindingByIDParams{
		ID: uuid(want), WorkspaceID: uuid(workspace),
	})
	if err != nil {
		translated := postgres.Translate(ctx, err, "finding: read")
		if errors.IsKind(translated, errors.NotFound) {
			return domain.Finding{}, fmt.Errorf("finding: read: %w", domain.ErrNotFound)
		}
		return domain.Finding{}, translated
	}
	return finding(row)
}

// Page is the board: worst first, then newest.
func (s *Store) Page(ctx context.Context, workspace id.ID, state string,
	fragment id.ID, limit int) ([]domain.Finding, error) {
	rows, err := s.q(ctx).FindingsForWorkspace(ctx, findingdb.FindingsForWorkspaceParams{
		WorkspaceID: uuid(workspace), State: text(state),
		Fragment: maybe(fragment), Page: int32(limit),
	})
	if err != nil {
		return nil, postgres.Translate(ctx, err, "finding: page")
	}
	out := make([]domain.Finding, 0, len(rows))
	for _, row := range rows {
		f, err := finding(row)
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, nil
}

func (s *Store) ForFragments(ctx context.Context, workspace id.ID, fragments []id.ID) ([]domain.Finding, error) {
	keys := make([]pgtype.UUID, 0, len(fragments))
	for _, fragment := range fragments {
		keys = append(keys, uuid(fragment))
	}
	rows, err := s.q(ctx).FindingsForFragments(ctx, findingdb.FindingsForFragmentsParams{
		WorkspaceID: uuid(workspace), Fragments: keys,
	})
	if err != nil {
		return nil, postgres.Translate(ctx, err, "finding: target fragments")
	}
	out := make([]domain.Finding, 0, len(rows))
	for _, row := range rows {
		f, err := finding(row)
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, nil
}

// Save writes what a PERSON changed. It deliberately touches no column the
// rescan upsert touches — splitting them is what stops a triage racing a scan
// into a lost update.
func (s *Store) Save(ctx context.Context, f domain.Finding) error {
	params := findingdb.SaveFindingParams{
		ID: uuid(f.ID), WorkspaceID: uuid(f.WorkspaceID),
		State: f.State.String(), Reason: text(f.Reason),
		DecidedBy: maybe(f.DecidedBy), DecidedAt: stamp(f.DecidedAt),
		Severity: f.Severity.String(),
		Claimant: f.Assessment.Claimant.String(), Actor: maybe(f.Assessment.Actor),
		Confidence: confidence(f.Assessment.Confidence, f.Assessment.HasConfidence),
		Basis:      text(f.Assessment.Basis), AssessedAt: stamp(f.Assessment.At),
	}
	if f.Superseded != nil {
		params.SupersededSeverity = text(f.SupersededSeverity.String())
		params.SupersededClaimant = text(f.Superseded.Claimant.String())
		params.SupersededActor = maybe(f.Superseded.Actor)
		params.SupersededConfidence = confidence(f.Superseded.Confidence, f.Superseded.HasConfidence)
		params.SupersededBasis = text(f.Superseded.Basis)
		params.SupersededAt = stamp(f.Superseded.At)
	}
	n, err := s.q(ctx).SaveFinding(ctx, params)
	if err != nil {
		return postgres.Translate(ctx, err, "finding: save")
	}
	if n == 0 {
		return fmt.Errorf("finding: save: %w", domain.ErrNotFound)
	}
	return nil
}

// Live is 0009's badge, counted per fragment. `worst` is the severity ORDER —
// 0 is critical — rather than the name, because the query sorts on it and
// translating it back here keeps one definition of the order in SQL.
func (s *Store) Live(ctx context.Context, workspace id.ID) (map[id.ID]domain.Badge, error) {
	rows, err := s.q(ctx).LiveFindingsPerFragment(ctx, uuid(workspace))
	if err != nil {
		return nil, postgres.Translate(ctx, err, "finding: live per fragment")
	}
	out := make(map[id.ID]domain.Badge, len(rows))
	for _, row := range rows {
		out[ident(row.FragmentID)] = domain.Badge{
			Live: int(row.Live), Worst: domain.SeverityAt(int(row.Worst)),
		}
	}
	return out, nil
}

func (s *Store) SaveDetail(ctx context.Context, d domain.Detail) error {
	err := s.q(ctx).UpsertDetail(ctx, findingdb.UpsertDetailParams{
		ID: uuid(d.ID), FindingID: uuid(d.FindingID), Field: d.Field, Value: d.Value,
		MappingID: uuid(d.MappingID), ArtifactID: uuid(d.ArtifactID),
		SeenAt: stamp(d.SeenAt),
	})
	if err != nil {
		return postgres.Translate(ctx, err, "finding: save detail")
	}
	return nil
}

func (s *Store) Details(ctx context.Context, finding id.ID) ([]domain.Detail, error) {
	rows, err := s.q(ctx).DetailsForFinding(ctx, uuid(finding))
	if err != nil {
		return nil, postgres.Translate(ctx, err, "finding: details")
	}
	out := make([]domain.Detail, 0, len(rows))
	for _, row := range rows {
		out = append(out, detail(row))
	}
	return out, nil
}
