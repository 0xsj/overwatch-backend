package domain

import (
	"strings"
	"time"
	"unicode/utf8"

	"github.com/0xsj/overwatch-backend/pkg/id"
)

const (
	EventGenerated             = "assistance.operation.generated"
	EventReviewed              = "assistance.proposal.reviewed"
	MaxProposals               = 12
	MaxCandidateName           = 400
	MaxCandidateDescription    = 4000
	MaxRelationshipDescription = 4000
	MaxOperationError          = 2000
)

type OperationStatus string

const (
	OperationCompleted   OperationStatus = "completed"
	OperationEmpty       OperationStatus = "empty"
	OperationPartial     OperationStatus = "partial"
	OperationFailed      OperationStatus = "failed"
	OperationUnsupported OperationStatus = "unsupported"
)

func (s OperationStatus) String() string { return string(s) }

func ParseOperationStatus(raw string) (OperationStatus, error) {
	switch OperationStatus(strings.TrimSpace(raw)) {
	case OperationCompleted, OperationEmpty, OperationPartial, OperationFailed, OperationUnsupported:
		return OperationStatus(strings.TrimSpace(raw)), nil
	default:
		return "", ErrStateUnknown
	}
}

type ProposalState string

const (
	ProposalProposed ProposalState = "proposed"
	ProposalAccepted ProposalState = "accepted"
	ProposalRejected ProposalState = "rejected"
)

func (s ProposalState) String() string { return string(s) }

func ParseProposalState(raw string) (ProposalState, error) {
	switch ProposalState(raw) {
	case ProposalProposed, ProposalAccepted, ProposalRejected:
		return ProposalState(raw), nil
	default:
		return "", ErrStateUnknown
	}
}

type ReviewDecision string

const (
	DecisionAccept ReviewDecision = "accept"
	DecisionReject ReviewDecision = "reject"
)

type Operation struct {
	ID              id.ID           `json:"operation_id"`
	WorkspaceID     id.ID           `json:"workspace_id"`
	SourceID        id.ID           `json:"source_id"`
	CaptureID       id.ID           `json:"capture_id"`
	ExtractionID    *id.ID          `json:"extraction_id,omitempty"`
	Status          OperationStatus `json:"status"`
	Provider        string          `json:"provider"`
	Method          string          `json:"method"`
	TemplateVersion string          `json:"template_version"`
	CreatedBy       id.ID           `json:"created_by"`
	CreatedAt       time.Time       `json:"created_at"`
	CompletedAt     time.Time       `json:"completed_at"`
	ProposalCount   int             `json:"proposal_count"`
	InputBytes      int64           `json:"input_bytes"`
	OutputBytes     int64           `json:"output_bytes"`
	DurationMS      int64           `json:"duration_ms"`
	TimedOut        bool            `json:"timed_out"`
	Error           string          `json:"error,omitempty"`
	RetryOf         *id.ID          `json:"retry_of,omitempty"`
}

type Proposal struct {
	ID                      id.ID         `json:"proposal_id"`
	OperationID             id.ID         `json:"operation_id"`
	WorkspaceID             id.ID         `json:"workspace_id"`
	SourceID                id.ID         `json:"source_id"`
	CaptureID               id.ID         `json:"capture_id"`
	ExtractionID            *id.ID        `json:"extraction_id,omitempty"`
	GeneratedStatement      string        `json:"generated_statement"`
	GeneratedQuote          string        `json:"generated_quote"`
	GeneratedQuoteStart     int           `json:"generated_quote_start"`
	GeneratedQuoteEnd       int           `json:"generated_quote_end"`
	CandidateKind           string        `json:"candidate_kind,omitempty"`
	CandidateName           string        `json:"candidate_name,omitempty"`
	CandidateDescription    string        `json:"candidate_description,omitempty"`
	RelationshipKind        string        `json:"relationship_kind,omitempty"`
	RelatedCandidateKind    string        `json:"related_candidate_kind,omitempty"`
	RelatedCandidateName    string        `json:"related_candidate_name,omitempty"`
	RelationshipDescription string        `json:"relationship_description,omitempty"`
	State                   ProposalState `json:"state"`
	ReviewedStatement       string        `json:"reviewed_statement,omitempty"`
	ReviewedQuote           string        `json:"reviewed_quote,omitempty"`
	ReviewedQuoteStart      *int          `json:"reviewed_quote_start,omitempty"`
	ReviewedQuoteEnd        *int          `json:"reviewed_quote_end,omitempty"`
	ReviewedBy              id.ID         `json:"reviewed_by,omitempty"`
	ReviewedAt              *time.Time    `json:"reviewed_at,omitempty"`
	ReviewNote              string        `json:"review_note,omitempty"`
	CreatedAt               time.Time     `json:"created_at"`
}

func (p Proposal) ReviewedAtValue() time.Time {
	if p.ReviewedAt == nil {
		return time.Time{}
	}
	return *p.ReviewedAt
}

type ProposalDraft struct {
	Statement               string
	Quote                   string
	QuoteStart              int
	QuoteEnd                int
	CandidateKind           string
	CandidateName           string
	CandidateDescription    string
	RelationshipKind        string
	RelatedCandidateKind    string
	RelatedCandidateName    string
	RelationshipDescription string
}

type Review struct {
	ID          id.ID
	ProposalID  id.ID
	WorkspaceID id.ID
	Decision    ReviewDecision
	Statement   string
	Quote       string
	QuoteStart  *int
	QuoteEnd    *int
	Note        string
	Reviewer    id.ID
	ReviewedAt  time.Time
}

func NewReview(want id.ID, proposal Proposal, reviewer id.ID, decision ReviewDecision, statement, quote string, start *int, note string, at time.Time) Review {
	var end *int
	if start != nil {
		value := *start + utf8.RuneCountInString(quote)
		end = &value
	}
	return Review{ID: want, ProposalID: proposal.ID, WorkspaceID: proposal.WorkspaceID, Decision: decision, Statement: strings.TrimSpace(statement), Quote: quote, QuoteStart: start, QuoteEnd: end, Note: strings.TrimSpace(note), Reviewer: reviewer, ReviewedAt: at}
}

func NewOperation(want, workspace, source, capture, actor id.ID, provider, method string, count int, at time.Time) (Operation, error) {
	return NewOperationResult(want, workspace, source, capture, actor, provider, method, OperationCompleted, "", count, nil, at)
}

func NewOperationResult(want, workspace, source, capture, actor id.ID, provider, method string, status OperationStatus, failure string, count int, retryOf *id.ID, at time.Time) (Operation, error) {
	if want.IsZero() || workspace.IsZero() || source.IsZero() || capture.IsZero() || actor.IsZero() {
		return Operation{}, ErrIDRequired
	}
	if strings.TrimSpace(provider) == "" || !utf8.ValidString(provider) || len(provider) > 200 {
		return Operation{}, ErrProviderRequired
	}
	if strings.TrimSpace(method) == "" || !utf8.ValidString(method) || len(method) > 200 {
		return Operation{}, ErrMethodRequired
	}
	if count < 0 || count > MaxProposals || at.IsZero() {
		return Operation{}, ErrProposalTooLong
	}
	parsed, err := ParseOperationStatus(status.String())
	if err != nil {
		return Operation{}, err
	}
	failure = strings.TrimSpace(failure)
	if len(failure) > MaxOperationError || !utf8.ValidString(failure) || strings.ContainsRune(failure, 0) {
		return Operation{}, ErrProposalTooLong
	}
	if (parsed == OperationFailed || parsed == OperationUnsupported) && failure == "" {
		return Operation{}, ErrProposalRequired
	}
	if parsed != OperationFailed && parsed != OperationUnsupported && parsed != OperationPartial && failure != "" {
		return Operation{}, ErrProposalRequired
	}
	if retryOf != nil && retryOf.IsZero() {
		return Operation{}, ErrIDRequired
	}
	var retry *id.ID
	if retryOf != nil {
		copyOf := *retryOf
		retry = &copyOf
	}
	return Operation{ID: want, WorkspaceID: workspace, SourceID: source, CaptureID: capture, Status: parsed, Provider: provider, Method: method, CreatedBy: actor, CreatedAt: at, CompletedAt: at, ProposalCount: count, Error: failure, RetryOf: retry}, nil
}

func NewProposal(want, operation, workspace, source, capture id.ID, draft ProposalDraft, at time.Time) (Proposal, error) {
	if want.IsZero() || operation.IsZero() || workspace.IsZero() || source.IsZero() || capture.IsZero() || at.IsZero() {
		return Proposal{}, ErrIDRequired
	}
	statement := strings.TrimSpace(draft.Statement)
	if statement == "" || len(statement) > 4000 || !utf8.ValidString(statement) || strings.ContainsRune(statement, 0) {
		return Proposal{}, ErrProposalRequired
	}
	if draft.Quote == "" || len(draft.Quote) > 8000 || !utf8.ValidString(draft.Quote) || strings.ContainsRune(draft.Quote, 0) || draft.QuoteStart < 0 || draft.QuoteEnd <= draft.QuoteStart {
		return Proposal{}, ErrProposalTooLong
	}
	kind, name, description, err := candidate(draft)
	if err != nil {
		return Proposal{}, err
	}
	relationshipKind, relatedKind, relatedName, relationshipDescription, err := relationship(draft, kind, name)
	if err != nil {
		return Proposal{}, err
	}
	return Proposal{ID: want, OperationID: operation, WorkspaceID: workspace, SourceID: source, CaptureID: capture, GeneratedStatement: statement, GeneratedQuote: draft.Quote, GeneratedQuoteStart: draft.QuoteStart, GeneratedQuoteEnd: draft.QuoteEnd, CandidateKind: kind, CandidateName: name, CandidateDescription: description, RelationshipKind: relationshipKind, RelatedCandidateKind: relatedKind, RelatedCandidateName: relatedName, RelationshipDescription: relationshipDescription, State: ProposalProposed, CreatedAt: at}, nil
}

func candidate(draft ProposalDraft) (string, string, string, error) {
	kind, name, description := strings.TrimSpace(draft.CandidateKind), strings.TrimSpace(draft.CandidateName), strings.TrimSpace(draft.CandidateDescription)
	if kind == "" && name == "" && description == "" {
		return "", "", "", nil
	}
	if !validCandidate(kind, name, MaxCandidateName) {
		return "", "", "", ErrCandidateInvalid
	}
	if len(description) > MaxCandidateDescription || !utf8.ValidString(description) || strings.ContainsRune(description, 0) {
		return "", "", "", ErrCandidateInvalid
	}
	return kind, name, description, nil
}

func relationship(draft ProposalDraft, candidateKind, candidateName string) (string, string, string, string, error) {
	kind := strings.TrimSpace(draft.RelationshipKind)
	relatedKind := strings.TrimSpace(draft.RelatedCandidateKind)
	relatedName := strings.TrimSpace(draft.RelatedCandidateName)
	description := strings.TrimSpace(draft.RelationshipDescription)
	if kind == "" && relatedKind == "" && relatedName == "" && description == "" {
		return "", "", "", "", nil
	}
	if candidateKind == "" || candidateName == "" || !validRelationship(kind) || !validCandidate(relatedKind, relatedName, MaxCandidateName) || len(description) > MaxRelationshipDescription || !utf8.ValidString(description) || strings.ContainsRune(description, 0) {
		return "", "", "", "", ErrCandidateInvalid
	}
	return kind, relatedKind, relatedName, description, nil
}

func validCandidate(kind, name string, nameLimit int) bool {
	return (kind == "person" || kind == "account" || kind == "organisation" || kind == "place") && name != "" && len(name) <= nameLimit && utf8.ValidString(name) && !strings.ContainsRune(name, 0)
}

func validRelationship(kind string) bool {
	switch kind {
	case "associated_with", "may_belong_to", "mentions", "concerns_same_event", "located_at", "possible_same_subject":
		return true
	default:
		return false
	}
}

func (p Proposal) Review(reviewer id.ID, decision ReviewDecision, statement, quote string, start *int, note, content string, at time.Time) (Proposal, error) {
	if reviewer.IsZero() {
		return Proposal{}, ErrIDRequired
	}
	if decision != DecisionAccept && decision != DecisionReject {
		return Proposal{}, ErrDecisionUnknown
	}
	if p.State != ProposalProposed {
		return Proposal{}, ErrStateUnknown
	}
	statement = strings.TrimSpace(statement)
	note = strings.TrimSpace(note)
	if len(note) > 2000 || !utf8.ValidString(note) || strings.ContainsRune(note, 0) {
		return Proposal{}, ErrProposalTooLong
	}
	next := p
	next.State = ProposalRejected
	if decision == DecisionAccept {
		if statement == "" {
			statement = p.GeneratedStatement
		}
		if statement == "" || len(statement) > 4000 || !utf8.ValidString(statement) || strings.ContainsRune(statement, 0) {
			return Proposal{}, ErrReviewRequired
		}
		if quote == "" {
			quote = p.GeneratedQuote
		}
		if !exactCitation(content, quote, start) {
			return Proposal{}, ErrCitation
		}
		offset := citationOffset(content, quote, start)
		end := offset + utf8.RuneCountInString(quote)
		next.State = ProposalAccepted
		next.ReviewedStatement, next.ReviewedQuote = statement, quote
		next.ReviewedQuoteStart, next.ReviewedQuoteEnd = &offset, &end
	}
	next.ReviewedBy, next.ReviewNote = reviewer, note
	next.ReviewedAt = &at
	return next, nil
}

func exactCitation(content, quote string, start *int) bool {
	if quote == "" || len(quote) > 8000 || !utf8.ValidString(content) || !utf8.ValidString(quote) {
		return false
	}
	runes := []rune(content)
	offset := citationOffset(content, quote, start)
	length := utf8.RuneCountInString(quote)
	return offset >= 0 && offset <= len(runes) && length <= len(runes)-offset && string(runes[offset:offset+length]) == quote
}

func citationOffset(content, quote string, start *int) int {
	if start != nil {
		return *start
	}
	byteOffset := strings.Index(content, quote)
	if byteOffset < 0 {
		return -1
	}
	return utf8.RuneCountInString(content[:byteOffset])
}
