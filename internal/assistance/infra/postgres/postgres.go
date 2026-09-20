package postgres

import (
	"context"
	"embed"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/0xsj/overwatch-backend/internal/assistance/domain"
	connectiondomain "github.com/0xsj/overwatch-backend/internal/researchconnection/domain"
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
 (id,workspace_id,source_id,capture_id,extraction_id,status,provider,method,template_version,created_by,created_at,completed_at,proposal_count,input_bytes,output_bytes,duration_ms,timed_out,error,retry_of)
values($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19)`, uuid(in.ID), uuid(in.WorkspaceID), uuid(in.SourceID), uuid(in.CaptureID), extraction, in.Status.String(), in.Provider, in.Method, in.TemplateVersion, uuid(in.CreatedBy), in.CreatedAt, in.CompletedAt, in.ProposalCount, in.InputBytes, in.OutputBytes, in.DurationMS, in.TimedOut, in.Error, optionalUUID(in.RetryOf))
	return translate(ctx, err)
}

func optionalUUID(value *id.ID) pgtype.UUID {
	if value == nil {
		return pgtype.UUID{}
	}
	return uuid(*value)
}

func (s *Store) ProviderPolicy(ctx context.Context, workspace id.ID) (domain.ProviderPolicy, error) {
	var policy domain.ProviderPolicy
	var space, actor pgtype.UUID
	err := s.db.DB(ctx).QueryRow(ctx, `select workspace_id,allow_external,updated_by,updated_at from assistance.provider_policy where workspace_id=$1`, uuid(workspace)).Scan(&space, &policy.AllowExternal, &actor, &policy.UpdatedAt)
	if err != nil {
		return domain.ProviderPolicy{}, translate(ctx, err)
	}
	policy.WorkspaceID, policy.UpdatedBy = id.ID(space.Bytes), id.ID(actor.Bytes)
	return policy, nil
}

func (s *Store) SaveProviderPolicy(ctx context.Context, policy domain.ProviderPolicy) error {
	if err := policy.Validate(); err != nil {
		return err
	}
	_, err := s.db.DB(ctx).Exec(ctx, `
insert into assistance.provider_policy(workspace_id,allow_external,updated_by,updated_at)
values($1,$2,$3,$4)
on conflict (workspace_id) do update set allow_external=excluded.allow_external,updated_by=excluded.updated_by,updated_at=excluded.updated_at`, uuid(policy.WorkspaceID), policy.AllowExternal, uuid(policy.UpdatedBy), policy.UpdatedAt)
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

type storedComparisonFinding struct {
	Kind           string   `json:"kind"`
	Summary        string   `json:"summary"`
	ObservationIDs []string `json:"observation_ids"`
}

func comparisonFindings(findings []domain.ComparisonFinding) []storedComparisonFinding {
	out := make([]storedComparisonFinding, 0, len(findings))
	for _, finding := range findings {
		out = append(out, storedComparisonFinding{Kind: finding.Kind.String(), Summary: finding.Summary, ObservationIDs: synthesisIDs(finding.ObservationIDs)})
	}
	return out
}

func (s *Store) CreateComparison(ctx context.Context, in domain.Comparison) error {
	observations, err := json.Marshal(synthesisIDs(in.ObservationIDs))
	if err != nil {
		return err
	}
	findings, err := json.Marshal(comparisonFindings(in.Findings))
	if err != nil {
		return err
	}
	_, err = s.db.DB(ctx).Exec(ctx, `insert into assistance.comparison(id,workspace_id,observation_ids,provider,method,template_version,status,output,findings,created_by,created_at) values($1,$2,$3::jsonb,$4,$5,$6,$7,$8,$9::jsonb,$10,$11)`, uuid(in.ID), uuid(in.WorkspaceID), string(observations), in.Provider, in.Method, in.TemplateVersion, in.Status.String(), in.Output, string(findings), uuid(in.CreatedBy), in.CreatedAt)
	return translate(ctx, err)
}

func connectionReviewFindings(findings []domain.ConnectionReviewFinding) []storedConnectionReviewFinding {
	out := make([]storedConnectionReviewFinding, 0, len(findings))
	for _, finding := range findings {
		out = append(out, storedConnectionReviewFinding{Kind: finding.Kind.String(), Summary: finding.Summary, ObservationIDs: synthesisIDs(finding.ObservationIDs)})
	}
	return out
}

type storedConnectionReviewFinding struct {
	Kind           string   `json:"kind"`
	Summary        string   `json:"summary"`
	ObservationIDs []string `json:"observation_ids"`
}

func (s *Store) CreateConnectionReview(ctx context.Context, in domain.ConnectionReview) error {
	supporting, err := json.Marshal(synthesisIDs(in.SupportingObservationIDs))
	if err != nil {
		return err
	}
	opposing, err := json.Marshal(synthesisIDs(in.OpposingObservationIDs))
	if err != nil {
		return err
	}
	findings, err := json.Marshal(connectionReviewFindings(in.Findings))
	if err != nil {
		return err
	}
	_, err = s.db.DB(ctx).Exec(ctx, `insert into assistance.connection_review(id,workspace_id,connection_id,from_record_id,to_record_id,connection_kind,connection_state,connection_rationale,supporting_observation_ids,opposing_observation_ids,provider,method,template_version,status,output,findings,created_by,created_at) values($1,$2,$3,$4,$5,$6,$7,$8,$9::jsonb,$10::jsonb,$11,$12,$13,$14,$15,$16::jsonb,$17,$18)`, uuid(in.ID), uuid(in.WorkspaceID), uuid(in.ConnectionID), uuid(in.FromRecordID), uuid(in.ToRecordID), in.ConnectionKind.String(), in.ConnectionState.String(), in.ConnectionRationale, string(supporting), string(opposing), in.Provider, in.Method, in.TemplateVersion, in.Status.String(), in.Output, string(findings), uuid(in.CreatedBy), in.CreatedAt)
	return translate(ctx, err)
}

type storedQuestionSuggestionGap struct {
	Kind           string   `json:"kind"`
	Label          string   `json:"label"`
	Detail         string   `json:"detail"`
	ObservationIDs []string `json:"observation_ids"`
}

type storedQuestionSuggestion struct {
	Kind           string   `json:"kind"`
	Prompt         string   `json:"prompt"`
	Context        string   `json:"context"`
	ObservationIDs []string `json:"observation_ids"`
}

func questionSuggestionGaps(gaps []domain.QuestionSuggestionGap) []storedQuestionSuggestionGap {
	out := make([]storedQuestionSuggestionGap, 0, len(gaps))
	for _, gap := range gaps {
		out = append(out, storedQuestionSuggestionGap{Kind: gap.Kind.String(), Label: gap.Label, Detail: gap.Detail, ObservationIDs: synthesisIDs(gap.ObservationIDs)})
	}
	return out
}

func questionSuggestions(suggestions []domain.QuestionSuggestion) []storedQuestionSuggestion {
	out := make([]storedQuestionSuggestion, 0, len(suggestions))
	for _, suggestion := range suggestions {
		out = append(out, storedQuestionSuggestion{Kind: suggestion.Kind.String(), Prompt: suggestion.Prompt, Context: suggestion.Context, ObservationIDs: synthesisIDs(suggestion.ObservationIDs)})
	}
	return out
}

func (s *Store) CreateQuestionSuggestions(ctx context.Context, in domain.QuestionSuggestions) error {
	gaps, err := json.Marshal(questionSuggestionGaps(in.Gaps))
	if err != nil {
		return err
	}
	suggestions, err := json.Marshal(questionSuggestions(in.Suggestions))
	if err != nil {
		return err
	}
	_, err = s.db.DB(ctx).Exec(ctx, `insert into assistance.question_suggestions(id,workspace_id,gaps,provider,method,template_version,status,output,suggestions,created_by,created_at) values($1,$2,$3::jsonb,$4,$5,$6,$7,$8,$9::jsonb,$10,$11)`, uuid(in.ID), uuid(in.WorkspaceID), string(gaps), in.Provider, in.Method, in.TemplateVersion, in.Status.String(), in.Output, string(suggestions), uuid(in.CreatedBy), in.CreatedAt)
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

const comparisonSelect = `select id,workspace_id,observation_ids,provider,method,template_version,status,output,findings,created_by,created_at from assistance.comparison `

func scanComparison(row interface{ Scan(...any) error }) (domain.Comparison, error) {
	var out domain.Comparison
	var comparison, workspace, createdBy pgtype.UUID
	var observationRaw, findingRaw []byte
	var status string
	if err := row.Scan(&comparison, &workspace, &observationRaw, &out.Provider, &out.Method, &out.TemplateVersion, &status, &out.Output, &findingRaw, &createdBy, &out.CreatedAt); err != nil {
		return domain.Comparison{}, err
	}
	parsedStatus, err := domain.ParseComparisonStatus(status)
	if err != nil {
		return domain.Comparison{}, err
	}
	var observations []string
	if err := json.Unmarshal(observationRaw, &observations); err != nil {
		return domain.Comparison{}, err
	}
	out.ObservationIDs = make([]id.ID, 0, len(observations))
	for _, raw := range observations {
		parsed, err := id.Parse(raw)
		if err != nil {
			return domain.Comparison{}, err
		}
		out.ObservationIDs = append(out.ObservationIDs, parsed)
	}
	var findings []storedComparisonFinding
	if err := json.Unmarshal(findingRaw, &findings); err != nil {
		return domain.Comparison{}, err
	}
	out.Findings = make([]domain.ComparisonFinding, 0, len(findings))
	for _, finding := range findings {
		kind, err := domain.ParseFindingKind(finding.Kind)
		if err != nil {
			return domain.Comparison{}, err
		}
		observationIDs := make([]id.ID, 0, len(finding.ObservationIDs))
		for _, raw := range finding.ObservationIDs {
			parsed, err := id.Parse(raw)
			if err != nil {
				return domain.Comparison{}, err
			}
			observationIDs = append(observationIDs, parsed)
		}
		out.Findings = append(out.Findings, domain.ComparisonFinding{Kind: kind, Summary: finding.Summary, ObservationIDs: observationIDs})
	}
	out.ID, out.WorkspaceID, out.Status, out.CreatedBy = id.ID(comparison.Bytes), id.ID(workspace.Bytes), parsedStatus, id.ID(createdBy.Bytes)
	return out, nil
}

func (s *Store) ComparisonByID(ctx context.Context, workspace, want id.ID) (domain.Comparison, error) {
	found, err := scanComparison(s.db.DB(ctx).QueryRow(ctx, comparisonSelect+`where workspace_id=$1 and id=$2`, uuid(workspace), uuid(want)))
	if err != nil {
		return domain.Comparison{}, translate(ctx, err)
	}
	return found, nil
}

func (s *Store) PageComparison(ctx context.Context, workspace, before id.ID, limit int) ([]domain.Comparison, error) {
	rows, err := s.db.DB(ctx).Query(ctx, comparisonSelect+`where workspace_id=$1 and ($2::uuid is null or id < $2) order by created_at desc, id desc limit $3`, uuid(workspace), uuid(before), limit)
	if err != nil {
		return nil, translate(ctx, err)
	}
	defer rows.Close()
	out := make([]domain.Comparison, 0)
	for rows.Next() {
		one, err := scanComparison(rows)
		if err != nil {
			return nil, translate(ctx, err)
		}
		out = append(out, one)
	}
	return out, translate(ctx, rows.Err())
}

const connectionReviewSelect = `select id,workspace_id,connection_id,from_record_id,to_record_id,connection_kind,connection_state,connection_rationale,supporting_observation_ids,opposing_observation_ids,provider,method,template_version,status,output,findings,created_by,created_at from assistance.connection_review `

func scanConnectionReview(row interface{ Scan(...any) error }) (domain.ConnectionReview, error) {
	var out domain.ConnectionReview
	var review, workspace, connection, fromRecord, toRecord, createdBy pgtype.UUID
	var kind, state, status string
	var supportingRaw, opposingRaw, findingRaw []byte
	if err := row.Scan(&review, &workspace, &connection, &fromRecord, &toRecord, &kind, &state, &out.ConnectionRationale, &supportingRaw, &opposingRaw, &out.Provider, &out.Method, &out.TemplateVersion, &status, &out.Output, &findingRaw, &createdBy, &out.CreatedAt); err != nil {
		return domain.ConnectionReview{}, err
	}
	parsedKind, err := connectiondomain.ParseKind(kind)
	if err != nil {
		return domain.ConnectionReview{}, err
	}
	parsedState, err := connectiondomain.ParseState(state)
	if err != nil {
		return domain.ConnectionReview{}, err
	}
	parsedStatus, err := domain.ParseConnectionReviewStatus(status)
	if err != nil {
		return domain.ConnectionReview{}, err
	}
	parseIDs := func(raw []byte) ([]id.ID, error) {
		var values []string
		if err := json.Unmarshal(raw, &values); err != nil {
			return nil, err
		}
		out := make([]id.ID, 0, len(values))
		for _, value := range values {
			parsed, err := id.Parse(value)
			if err != nil {
				return nil, err
			}
			out = append(out, parsed)
		}
		return out, nil
	}
	out.SupportingObservationIDs, err = parseIDs(supportingRaw)
	if err != nil {
		return domain.ConnectionReview{}, err
	}
	out.OpposingObservationIDs, err = parseIDs(opposingRaw)
	if err != nil {
		return domain.ConnectionReview{}, err
	}
	var findings []storedConnectionReviewFinding
	if err := json.Unmarshal(findingRaw, &findings); err != nil {
		return domain.ConnectionReview{}, err
	}
	out.Findings = make([]domain.ConnectionReviewFinding, 0, len(findings))
	for _, finding := range findings {
		parsedFinding, err := domain.ParseConnectionReviewFindingKind(finding.Kind)
		if err != nil {
			return domain.ConnectionReview{}, err
		}
		citations, err := parseIDs(mustJSON(finding.ObservationIDs))
		if err != nil {
			return domain.ConnectionReview{}, err
		}
		out.Findings = append(out.Findings, domain.ConnectionReviewFinding{Kind: parsedFinding, Summary: finding.Summary, ObservationIDs: citations})
	}
	out.ID, out.WorkspaceID, out.ConnectionID, out.FromRecordID, out.ToRecordID, out.CreatedBy = id.ID(review.Bytes), id.ID(workspace.Bytes), id.ID(connection.Bytes), id.ID(fromRecord.Bytes), id.ID(toRecord.Bytes), id.ID(createdBy.Bytes)
	out.ConnectionKind, out.ConnectionState, out.Status = parsedKind, parsedState, parsedStatus
	return out, nil
}

func mustJSON(values []string) []byte {
	raw, _ := json.Marshal(values)
	return raw
}

func (s *Store) ConnectionReviewByID(ctx context.Context, workspace, connection, want id.ID) (domain.ConnectionReview, error) {
	found, err := scanConnectionReview(s.db.DB(ctx).QueryRow(ctx, connectionReviewSelect+`where workspace_id=$1 and connection_id=$2 and id=$3`, uuid(workspace), uuid(connection), uuid(want)))
	if err != nil {
		return domain.ConnectionReview{}, translate(ctx, err)
	}
	return found, nil
}

func (s *Store) PageConnectionReviews(ctx context.Context, workspace, connection, before id.ID, limit int) ([]domain.ConnectionReview, error) {
	rows, err := s.db.DB(ctx).Query(ctx, connectionReviewSelect+`where workspace_id=$1 and connection_id=$2 and ($3::uuid is null or id < $3) order by created_at desc, id desc limit $4`, uuid(workspace), uuid(connection), uuid(before), limit)
	if err != nil {
		return nil, translate(ctx, err)
	}
	defer rows.Close()
	out := make([]domain.ConnectionReview, 0)
	for rows.Next() {
		one, err := scanConnectionReview(rows)
		if err != nil {
			return nil, translate(ctx, err)
		}
		out = append(out, one)
	}
	return out, translate(ctx, rows.Err())
}

const questionSuggestionsSelect = `select id,workspace_id,gaps,provider,method,template_version,status,output,suggestions,created_by,created_at from assistance.question_suggestions `

func scanQuestionSuggestions(row interface{ Scan(...any) error }) (domain.QuestionSuggestions, error) {
	var out domain.QuestionSuggestions
	var suggestion, workspace, createdBy pgtype.UUID
	var gapsRaw, suggestionsRaw []byte
	var status string
	if err := row.Scan(&suggestion, &workspace, &gapsRaw, &out.Provider, &out.Method, &out.TemplateVersion, &status, &out.Output, &suggestionsRaw, &createdBy, &out.CreatedAt); err != nil {
		return domain.QuestionSuggestions{}, err
	}
	parsedStatus, err := domain.ParseQuestionSuggestionStatus(status)
	if err != nil {
		return domain.QuestionSuggestions{}, err
	}
	var storedGaps []storedQuestionSuggestionGap
	if err := json.Unmarshal(gapsRaw, &storedGaps); err != nil {
		return domain.QuestionSuggestions{}, err
	}
	parseIDs := func(values []string) ([]id.ID, error) {
		out := make([]id.ID, 0, len(values))
		for _, value := range values {
			parsed, err := id.Parse(value)
			if err != nil {
				return nil, err
			}
			out = append(out, parsed)
		}
		return out, nil
	}
	out.Gaps = make([]domain.QuestionSuggestionGap, 0, len(storedGaps))
	for _, gap := range storedGaps {
		kind, err := domain.ParseQuestionGapKind(gap.Kind)
		if err != nil {
			return domain.QuestionSuggestions{}, err
		}
		observations, err := parseIDs(gap.ObservationIDs)
		if err != nil {
			return domain.QuestionSuggestions{}, err
		}
		out.Gaps = append(out.Gaps, domain.QuestionSuggestionGap{Kind: kind, Label: gap.Label, Detail: gap.Detail, ObservationIDs: observations})
	}
	var storedSuggestions []storedQuestionSuggestion
	if err := json.Unmarshal(suggestionsRaw, &storedSuggestions); err != nil {
		return domain.QuestionSuggestions{}, err
	}
	out.Suggestions = make([]domain.QuestionSuggestion, 0, len(storedSuggestions))
	for _, one := range storedSuggestions {
		kind, err := domain.ParseQuestionGapKind(one.Kind)
		if err != nil {
			return domain.QuestionSuggestions{}, err
		}
		observations, err := parseIDs(one.ObservationIDs)
		if err != nil {
			return domain.QuestionSuggestions{}, err
		}
		out.Suggestions = append(out.Suggestions, domain.QuestionSuggestion{Kind: kind, Prompt: one.Prompt, Context: one.Context, ObservationIDs: observations})
	}
	out.ID, out.WorkspaceID, out.CreatedBy, out.Status = id.ID(suggestion.Bytes), id.ID(workspace.Bytes), id.ID(createdBy.Bytes), parsedStatus
	return out, nil
}

func (s *Store) QuestionSuggestionsByID(ctx context.Context, workspace, want id.ID) (domain.QuestionSuggestions, error) {
	found, err := scanQuestionSuggestions(s.db.DB(ctx).QueryRow(ctx, questionSuggestionsSelect+`where workspace_id=$1 and id=$2`, uuid(workspace), uuid(want)))
	if err != nil {
		return domain.QuestionSuggestions{}, translate(ctx, err)
	}
	return found, nil
}

func (s *Store) PageQuestionSuggestions(ctx context.Context, workspace, before id.ID, limit int) ([]domain.QuestionSuggestions, error) {
	rows, err := s.db.DB(ctx).Query(ctx, questionSuggestionsSelect+`where workspace_id=$1 and ($2::uuid is null or id < $2) order by created_at desc, id desc limit $3`, uuid(workspace), uuid(before), limit)
	if err != nil {
		return nil, translate(ctx, err)
	}
	defer rows.Close()
	out := make([]domain.QuestionSuggestions, 0)
	for rows.Next() {
		one, err := scanQuestionSuggestions(rows)
		if err != nil {
			return nil, translate(ctx, err)
		}
		out = append(out, one)
	}
	return out, translate(ctx, rows.Err())
}

type storedBriefDraftInput struct {
	BriefID        string   `json:"brief_id"`
	Title          string   `json:"title"`
	Question       string   `json:"question"`
	CurrentAccount string   `json:"current_account,omitempty"`
	Alternatives   string   `json:"alternatives,omitempty"`
	Limitations    string   `json:"limitations,omitempty"`
	NextSteps      string   `json:"next_steps,omitempty"`
	ObservationIDs []string `json:"observation_ids"`
}

type storedBriefDraftChange struct {
	Section        string   `json:"section"`
	Before         string   `json:"before"`
	After          string   `json:"after"`
	Rationale      string   `json:"rationale"`
	ObservationIDs []string `json:"observation_ids"`
}

func briefDraftInput(input domain.BriefDraftInput) storedBriefDraftInput {
	return storedBriefDraftInput{BriefID: input.BriefID.String(), Title: input.Title, Question: input.Question, CurrentAccount: input.CurrentAccount, Alternatives: input.Alternatives, Limitations: input.Limitations, NextSteps: input.NextSteps, ObservationIDs: synthesisIDs(input.ObservationIDs)}
}

func briefDraftChanges(changes []domain.BriefDraftChange) []storedBriefDraftChange {
	out := make([]storedBriefDraftChange, 0, len(changes))
	for _, change := range changes {
		out = append(out, storedBriefDraftChange{Section: change.Section.String(), Before: change.Before, After: change.After, Rationale: change.Rationale, ObservationIDs: synthesisIDs(change.ObservationIDs)})
	}
	return out
}

func (s *Store) CreateBriefDraft(ctx context.Context, in domain.BriefDraft) error {
	input, err := json.Marshal(briefDraftInput(in.Input))
	if err != nil {
		return err
	}
	changes, err := json.Marshal(briefDraftChanges(in.Changes))
	if err != nil {
		return err
	}
	_, err = s.db.DB(ctx).Exec(ctx, `insert into assistance.brief_draft(id,workspace_id,brief_id,input,provider,method,template_version,status,output,changes,created_by,created_at) values($1,$2,$3,$4::jsonb,$5,$6,$7,$8,$9,$10::jsonb,$11,$12)`, uuid(in.ID), uuid(in.WorkspaceID), uuid(in.Input.BriefID), string(input), in.Provider, in.Method, in.TemplateVersion, in.Status.String(), in.Output, string(changes), uuid(in.CreatedBy), in.CreatedAt)
	return translate(ctx, err)
}

const briefDraftSelect = `select id,workspace_id,brief_id,input,provider,method,template_version,status,output,changes,created_by,created_at from assistance.brief_draft `

func scanBriefDraft(row interface{ Scan(...any) error }) (domain.BriefDraft, error) {
	var out domain.BriefDraft
	var draft, workspace, briefID, createdBy pgtype.UUID
	var inputRaw, changesRaw []byte
	var status string
	if err := row.Scan(&draft, &workspace, &briefID, &inputRaw, &out.Provider, &out.Method, &out.TemplateVersion, &status, &out.Output, &changesRaw, &createdBy, &out.CreatedAt); err != nil {
		return domain.BriefDraft{}, err
	}
	parsedStatus, err := domain.ParseBriefDraftStatus(status)
	if err != nil {
		return domain.BriefDraft{}, err
	}
	var storedInput storedBriefDraftInput
	if err := json.Unmarshal(inputRaw, &storedInput); err != nil {
		return domain.BriefDraft{}, err
	}
	parsedBriefID, err := id.Parse(storedInput.BriefID)
	if err != nil {
		return domain.BriefDraft{}, err
	}
	parseIDs := func(values []string) ([]id.ID, error) {
		out := make([]id.ID, 0, len(values))
		for _, value := range values {
			parsed, err := id.Parse(value)
			if err != nil {
				return nil, err
			}
			out = append(out, parsed)
		}
		return out, nil
	}
	inputObservationIDs, err := parseIDs(storedInput.ObservationIDs)
	if err != nil {
		return domain.BriefDraft{}, err
	}
	out.Input = domain.BriefDraftInput{BriefID: parsedBriefID, Title: storedInput.Title, Question: storedInput.Question, CurrentAccount: storedInput.CurrentAccount, Alternatives: storedInput.Alternatives, Limitations: storedInput.Limitations, NextSteps: storedInput.NextSteps, ObservationIDs: inputObservationIDs}
	var storedChanges []storedBriefDraftChange
	if err := json.Unmarshal(changesRaw, &storedChanges); err != nil {
		return domain.BriefDraft{}, err
	}
	out.Changes = make([]domain.BriefDraftChange, 0, len(storedChanges))
	for _, change := range storedChanges {
		section, err := domain.ParseBriefDraftSection(change.Section)
		if err != nil {
			return domain.BriefDraft{}, err
		}
		observationIDs, err := parseIDs(change.ObservationIDs)
		if err != nil {
			return domain.BriefDraft{}, err
		}
		out.Changes = append(out.Changes, domain.BriefDraftChange{Section: section, Before: change.Before, After: change.After, Rationale: change.Rationale, ObservationIDs: observationIDs})
	}
	out.ID, out.WorkspaceID, out.CreatedBy, out.Status = id.ID(draft.Bytes), id.ID(workspace.Bytes), id.ID(createdBy.Bytes), parsedStatus
	return out, nil
}

func (s *Store) BriefDraftByID(ctx context.Context, workspace, want id.ID) (domain.BriefDraft, error) {
	found, err := scanBriefDraft(s.db.DB(ctx).QueryRow(ctx, briefDraftSelect+`where workspace_id=$1 and id=$2`, uuid(workspace), uuid(want)))
	if err != nil {
		return domain.BriefDraft{}, translate(ctx, err)
	}
	return found, nil
}

func (s *Store) PageBriefDrafts(ctx context.Context, workspace, before id.ID, limit int) ([]domain.BriefDraft, error) {
	rows, err := s.db.DB(ctx).Query(ctx, briefDraftSelect+`where workspace_id=$1 and ($2::uuid is null or id < $2) order by created_at desc, id desc limit $3`, uuid(workspace), uuid(before), limit)
	if err != nil {
		return nil, translate(ctx, err)
	}
	defer rows.Close()
	out := make([]domain.BriefDraft, 0)
	for rows.Next() {
		one, err := scanBriefDraft(rows)
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

const operationSelect = `select id,workspace_id,source_id,capture_id,extraction_id,status,provider,method,template_version,created_by,created_at,completed_at,proposal_count,input_bytes,output_bytes,duration_ms,timed_out,error,retry_of from assistance.operation `

func scanOperation(ctx context.Context, row interface{ Scan(...any) error }) (domain.Operation, error) {
	var out domain.Operation
	var operation, space, source, capture, extraction, actor, retryOf pgtype.UUID
	var status string
	err := row.Scan(&operation, &space, &source, &capture, &extraction, &status, &out.Provider, &out.Method, &out.TemplateVersion, &actor, &out.CreatedAt, &out.CompletedAt, &out.ProposalCount, &out.InputBytes, &out.OutputBytes, &out.DurationMS, &out.TimedOut, &out.Error, &retryOf)
	if err != nil {
		return domain.Operation{}, translate(ctx, err)
	}
	parsed, err := domain.ParseOperationStatus(status)
	if err != nil {
		return domain.Operation{}, err
	}
	out.ID, out.WorkspaceID, out.SourceID, out.CaptureID, out.CreatedBy, out.Status = id.ID(operation.Bytes), id.ID(space.Bytes), id.ID(source.Bytes), id.ID(capture.Bytes), id.ID(actor.Bytes), parsed
	if extraction.Valid {
		value := id.ID(extraction.Bytes)
		out.ExtractionID = &value
	}
	if retryOf.Valid {
		value := id.ID(retryOf.Bytes)
		out.RetryOf = &value
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
