package postgres

import (
	"context"
	"embed"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/0xsj/overwatch-backend/internal/assistance/domain"
	"github.com/0xsj/overwatch-backend/pkg/id"
	"github.com/0xsj/overwatch-backend/pkg/postgres"
)

const Schema = "assistance"

//go:embed migrations/*.sql
var migrationFS embed.FS

var Migrations = mustLoad()

func mustLoad() []postgres.Migration {
	ms, err := postgres.FromFS(migrationFS, "migrations")
	if err != nil {
		panic("assistance: " + err.Error())
	}
	return ms
}

type Store struct{ db *postgres.Pool }

func NewStore(db *postgres.Pool) *Store {
	if db == nil {
		panic("assistance: NewStore with a nil pool")
	}
	return &Store{db: db}
}

func uuid(i id.ID) pgtype.UUID { return pgtype.UUID{Bytes: i, Valid: !i.IsZero()} }

func translate(ctx context.Context, err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrNotFound
	}
	return postgres.Translate(ctx, err, "assistance")
}

func (s *Store) CreateOperation(ctx context.Context, in domain.Operation) error {
	var extraction pgtype.UUID
	if in.ExtractionID != nil {
		extraction = uuid(*in.ExtractionID)
	}
	_, err := s.db.DB(ctx).Exec(ctx, `
insert into assistance.operation
 (id,workspace_id,source_id,capture_id,extraction_id,status,provider,method,created_by,created_at,completed_at,proposal_count)
values($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`, uuid(in.ID), uuid(in.WorkspaceID), uuid(in.SourceID), uuid(in.CaptureID), extraction, in.Status.String(), in.Provider, in.Method, uuid(in.CreatedBy), in.CreatedAt, in.CompletedAt, in.ProposalCount)
	return translate(ctx, err)
}

type storedSynthesisCandidate struct {
	Kind           string   `json:"kind"`
	Name           string   `json:"name"`
	ObservationIDs []string `json:"observation_ids"`
	Rationale      string   `json:"rationale"`
}

func synthesisIDs(ids []id.ID) []string {
	out := make([]string, 0, len(ids))
	for _, one := range ids {
		out = append(out, one.String())
	}
	return out
}

func synthesisCandidates(candidates []domain.SynthesisCandidate) []storedSynthesisCandidate {
	out := make([]storedSynthesisCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		out = append(out, storedSynthesisCandidate{Kind: candidate.Kind, Name: candidate.Name, ObservationIDs: synthesisIDs(candidate.ObservationIDs), Rationale: candidate.Rationale})
	}
	return out
}

func (s *Store) CreateSynthesis(ctx context.Context, in domain.Synthesis) error {
	observations, err := json.Marshal(synthesisIDs(in.ObservationIDs))
	if err != nil {
		return err
	}
	candidates, err := json.Marshal(synthesisCandidates(in.Candidates))
	if err != nil {
		return err
	}
	_, err = s.db.DB(ctx).Exec(ctx, `insert into assistance.synthesis(id,workspace_id,observation_ids,provider,method,output,candidates,created_by,created_at) values($1,$2,$3::jsonb,$4,$5,$6,$7::jsonb,$8,$9)`, uuid(in.ID), uuid(in.WorkspaceID), string(observations), in.Provider, in.Method, in.Output, string(candidates), uuid(in.CreatedBy), in.CreatedAt)
	return translate(ctx, err)
}

func (s *Store) CreateProposal(ctx context.Context, in domain.Proposal) error {
	var extraction pgtype.UUID
	if in.ExtractionID != nil {
		extraction = uuid(*in.ExtractionID)
	}
	_, err := s.db.DB(ctx).Exec(ctx, `
insert into assistance.proposal
	 (id,operation_id,workspace_id,source_id,capture_id,extraction_id,generated_statement,generated_quote,generated_quote_start,generated_quote_end,candidate_kind,candidate_name,candidate_description,relationship_kind,related_candidate_kind,related_candidate_name,relationship_description,state,created_at)
values($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19)`, uuid(in.ID), uuid(in.OperationID), uuid(in.WorkspaceID), uuid(in.SourceID), uuid(in.CaptureID), extraction, in.GeneratedStatement, []byte(in.GeneratedQuote), in.GeneratedQuoteStart, in.GeneratedQuoteEnd, in.CandidateKind, in.CandidateName, in.CandidateDescription, in.RelationshipKind, in.RelatedCandidateKind, in.RelatedCandidateName, in.RelationshipDescription, in.State.String(), in.CreatedAt)
	return translate(ctx, err)
}

func (s *Store) ByOperation(ctx context.Context, workspace, want id.ID) (domain.Operation, error) {
	return s.readOperation(ctx, `where workspace_id=$1 and id=$2`, uuid(workspace), uuid(want))
}

const synthesisSelect = `select id,workspace_id,observation_ids,provider,method,output,candidates,created_by,created_at from assistance.synthesis `

func scanSynthesis(row interface{ Scan(...any) error }) (domain.Synthesis, error) {
	var out domain.Synthesis
	var synthesis, workspace, createdBy pgtype.UUID
	var observationRaw, candidateRaw []byte
	if err := row.Scan(&synthesis, &workspace, &observationRaw, &out.Provider, &out.Method, &out.Output, &candidateRaw, &createdBy, &out.CreatedAt); err != nil {
		return domain.Synthesis{}, err
	}
	var observations []string
	if err := json.Unmarshal(observationRaw, &observations); err != nil {
		return domain.Synthesis{}, err
	}
	out.ObservationIDs = make([]id.ID, 0, len(observations))
	for _, raw := range observations {
		parsed, err := id.Parse(raw)
		if err != nil {
			return domain.Synthesis{}, err
		}
		out.ObservationIDs = append(out.ObservationIDs, parsed)
	}
	var candidates []storedSynthesisCandidate
	if err := json.Unmarshal(candidateRaw, &candidates); err != nil {
		return domain.Synthesis{}, err
	}
	out.Candidates = make([]domain.SynthesisCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		parsedIDs := make([]id.ID, 0, len(candidate.ObservationIDs))
		for _, raw := range candidate.ObservationIDs {
			parsed, err := id.Parse(raw)
			if err != nil {
				return domain.Synthesis{}, err
			}
			parsedIDs = append(parsedIDs, parsed)
		}
		out.Candidates = append(out.Candidates, domain.SynthesisCandidate{Kind: candidate.Kind, Name: candidate.Name, ObservationIDs: parsedIDs, Rationale: candidate.Rationale})
	}
	out.ID, out.WorkspaceID, out.CreatedBy = id.ID(synthesis.Bytes), id.ID(workspace.Bytes), id.ID(createdBy.Bytes)
	return out, nil
}

func (s *Store) SynthesisByID(ctx context.Context, workspace, want id.ID) (domain.Synthesis, error) {
	found, err := scanSynthesis(s.db.DB(ctx).QueryRow(ctx, synthesisSelect+`where workspace_id=$1 and id=$2`, uuid(workspace), uuid(want)))
	if err != nil {
		return domain.Synthesis{}, translate(ctx, err)
	}
	return found, nil
}

func (s *Store) PageSynthesis(ctx context.Context, workspace, before id.ID, limit int) ([]domain.Synthesis, error) {
	rows, err := s.db.DB(ctx).Query(ctx, synthesisSelect+`where workspace_id=$1 and ($2::uuid is null or id < $2) order by created_at desc, id desc limit $3`, uuid(workspace), uuid(before), limit)
	if err != nil {
		return nil, translate(ctx, err)
	}
	defer rows.Close()
	out := make([]domain.Synthesis, 0)
	for rows.Next() {
		one, err := scanSynthesis(rows)
		if err != nil {
			return nil, translate(ctx, err)
		}
		out = append(out, one)
	}
	return out, translate(ctx, rows.Err())
}

func (s *Store) LatestByCapture(ctx context.Context, workspace, source, capture, extraction id.ID) (domain.Operation, error) {
	return s.readOperation(ctx, `where workspace_id=$1 and source_id=$2 and capture_id=$3 and extraction_id is not distinct from $4 order by created_at desc, id desc limit 1`, uuid(workspace), uuid(source), uuid(capture), uuid(extraction))
}

func (s *Store) ByCapture(ctx context.Context, workspace, source, capture, extraction id.ID, limit int) ([]domain.Operation, error) {
	rows, err := s.db.DB(ctx).Query(ctx, operationSelect+`where workspace_id=$1 and source_id=$2 and capture_id=$3 and extraction_id is not distinct from $4 order by created_at desc, id desc limit $5`, uuid(workspace), uuid(source), uuid(capture), uuid(extraction), limit)
	if err != nil {
		return nil, translate(ctx, err)
	}
	defer rows.Close()
	out := make([]domain.Operation, 0)
	for rows.Next() {
		one, err := scanOperation(ctx, rows)
		if err != nil {
			return nil, translate(ctx, err)
		}
		out = append(out, one)
	}
	return out, translate(ctx, rows.Err())
}

func (s *Store) readOperation(ctx context.Context, clause string, args ...any) (domain.Operation, error) {
	return scanOperation(ctx, s.db.DB(ctx).QueryRow(ctx, operationSelect+clause, args...))
}

const operationSelect = `select id,workspace_id,source_id,capture_id,extraction_id,status,provider,method,created_by,created_at,completed_at,proposal_count from assistance.operation `

func scanOperation(ctx context.Context, row interface{ Scan(...any) error }) (domain.Operation, error) {
	var out domain.Operation
	var operation, space, source, capture, extraction, actor pgtype.UUID
	var status string
	err := row.Scan(&operation, &space, &source, &capture, &extraction, &status, &out.Provider, &out.Method, &actor, &out.CreatedAt, &out.CompletedAt, &out.ProposalCount)
	if err != nil {
		return domain.Operation{}, translate(ctx, err)
	}
	parsed := domain.OperationStatus(status)
	out.ID, out.WorkspaceID, out.SourceID, out.CaptureID, out.CreatedBy, out.Status = id.ID(operation.Bytes), id.ID(space.Bytes), id.ID(source.Bytes), id.ID(capture.Bytes), id.ID(actor.Bytes), parsed
	if extraction.Valid {
		value := id.ID(extraction.Bytes)
		out.ExtractionID = &value
	}
	return out, nil
}

func scanProposal(row interface{ Scan(...any) error }) (domain.Proposal, error) {
	var out domain.Proposal
	var proposal, operation, space, source, capture, extraction, reviewer pgtype.UUID
	var state string
	var generatedQuote, reviewedQuote []byte
	var reviewedStart, reviewedEnd pgtype.Int4
	if err := row.Scan(&proposal, &operation, &space, &source, &capture, &extraction, &out.GeneratedStatement, &generatedQuote, &out.GeneratedQuoteStart, &out.GeneratedQuoteEnd, &out.CandidateKind, &out.CandidateName, &out.CandidateDescription, &out.RelationshipKind, &out.RelatedCandidateKind, &out.RelatedCandidateName, &out.RelationshipDescription, &state, &out.ReviewedStatement, &reviewedQuote, &reviewedStart, &reviewedEnd, &reviewer, &out.ReviewedAt, &out.ReviewNote, &out.CreatedAt); err != nil {
		return domain.Proposal{}, err
	}
	parsed, err := domain.ParseProposalState(state)
	if err != nil {
		return domain.Proposal{}, err
	}
	out.ID, out.OperationID, out.WorkspaceID, out.SourceID, out.CaptureID = id.ID(proposal.Bytes), id.ID(operation.Bytes), id.ID(space.Bytes), id.ID(source.Bytes), id.ID(capture.Bytes)
	out.GeneratedQuote, out.ReviewedQuote, out.State, out.ReviewedBy = string(generatedQuote), string(reviewedQuote), parsed, id.ID(reviewer.Bytes)
	if extraction.Valid {
		value := id.ID(extraction.Bytes)
		out.ExtractionID = &value
	}
	if reviewedStart.Valid {
		value := int(reviewedStart.Int32)
		out.ReviewedQuoteStart = &value
	}
	if reviewedEnd.Valid {
		value := int(reviewedEnd.Int32)
		out.ReviewedQuoteEnd = &value
	}
	return out, nil
}

func (s *Store) ByProposal(ctx context.Context, workspace, want id.ID) (domain.Proposal, error) {
	return scanProposal(s.db.DB(ctx).QueryRow(ctx, proposalSelect+` where p.workspace_id=$1 and p.id=$2`, uuid(workspace), uuid(want)))
}

const proposalSelect = `select p.id,p.operation_id,p.workspace_id,p.source_id,p.capture_id,p.extraction_id,p.generated_statement,p.generated_quote,p.generated_quote_start,p.generated_quote_end,p.candidate_kind,p.candidate_name,p.candidate_description,p.relationship_kind,p.related_candidate_kind,p.related_candidate_name,p.relationship_description,p.state,p.reviewed_statement,p.reviewed_quote,p.reviewed_quote_start,p.reviewed_quote_end,p.reviewed_by,p.reviewed_at,p.review_note,p.created_at from assistance.proposal p`

func (s *Store) Proposals(ctx context.Context, workspace, operation id.ID) ([]domain.Proposal, error) {
	rows, err := s.db.DB(ctx).Query(ctx, proposalSelect+` where p.workspace_id=$1 and p.operation_id=$2 order by p.created_at,p.id`, uuid(workspace), uuid(operation))
	if err != nil {
		return nil, translate(ctx, err)
	}
	defer rows.Close()
	out := make([]domain.Proposal, 0)
	for rows.Next() {
		one, err := scanProposal(rows)
		if err != nil {
			return nil, translate(ctx, err)
		}
		out = append(out, one)
	}
	return out, translate(ctx, rows.Err())
}

func (s *Store) SaveProposal(ctx context.Context, in domain.Proposal) error {
	tag, err := s.db.DB(ctx).Exec(ctx, `update assistance.proposal set state=$3,reviewed_statement=$4,reviewed_quote=$5,reviewed_quote_start=$6,reviewed_quote_end=$7,reviewed_by=$8,reviewed_at=$9,review_note=$10 where workspace_id=$1 and id=$2`, uuid(in.WorkspaceID), uuid(in.ID), in.State.String(), in.ReviewedStatement, []byte(in.ReviewedQuote), nullableInt(in.ReviewedQuoteStart), nullableInt(in.ReviewedQuoteEnd), uuid(in.ReviewedBy), in.ReviewedAt, in.ReviewNote)
	if err != nil {
		return translate(ctx, err)
	}
	if tag.RowsAffected() != 1 {
		return domain.ErrNotFound
	}
	return nil
}

func nullableInt(in *int) pgtype.Int4 {
	if in == nil {
		return pgtype.Int4{}
	}
	return pgtype.Int4{Int32: int32(*in), Valid: true}
}

func (s *Store) CreateReview(ctx context.Context, in domain.Review) error {
	_, err := s.db.DB(ctx).Exec(ctx, `insert into assistance.proposal_review(id,proposal_id,workspace_id,decision,statement,quote,quote_start,quote_end,note,reviewer,reviewed_at) values($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`, uuid(in.ID), uuid(in.ProposalID), uuid(in.WorkspaceID), string(in.Decision), in.Statement, []byte(in.Quote), nullableInt(in.QuoteStart), nullableInt(in.QuoteEnd), in.Note, uuid(in.Reviewer), in.ReviewedAt)
	return translate(ctx, err)
}
