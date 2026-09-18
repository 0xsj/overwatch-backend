package domain_test

import (
	"testing"
	"time"

	"github.com/0xsj/overwatch-backend/internal/assistance/domain"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

func aid(value byte) id.ID { var out id.ID; out[15] = value; return out }

func TestReviewPreservesGeneratedOutputAndUsesRuneOffsets(t *testing.T) {
	at := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)
	quote := "📍 East Quay"
	one, err := domain.NewProposal(aid(1), aid(2), aid(3), aid(4), aid(5), domain.ProposalDraft{
		Statement: quote, Quote: quote, QuoteStart: 2, QuoteEnd: 13,
	}, at)
	if err != nil {
		t.Fatal(err)
	}
	start := 2
	accepted, err := one.Review(aid(6), domain.DecisionAccept, "The notice names East Quay.", quote, &start, "confirmed by analyst", "\n\n"+quote+".", at)
	if err != nil {
		t.Fatal(err)
	}
	if accepted.State != domain.ProposalAccepted || accepted.GeneratedQuote != quote || accepted.GeneratedStatement != quote {
		t.Fatalf("generated output changed during review: %+v", accepted)
	}
	if accepted.ReviewedQuoteStart == nil || *accepted.ReviewedQuoteStart != 2 || accepted.ReviewedQuoteEnd == nil || *accepted.ReviewedQuoteEnd != 13 {
		t.Fatalf("reviewed rune offsets were not retained: %+v", accepted)
	}
	wrong := 3
	if _, err := one.Review(aid(6), domain.DecisionAccept, "Claim", quote, &wrong, "", "\n\n"+quote+".", at); err != domain.ErrCitation {
		t.Fatalf("wrong offset error=%v, want citation error", err)
	}
}

func TestRejectedProposalRetainsGeneratedFields(t *testing.T) {
	at := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)
	one, err := domain.NewProposal(aid(11), aid(12), aid(13), aid(14), aid(15), domain.ProposalDraft{
		Statement: "A source sentence.", Quote: "A source sentence.", QuoteStart: 0, QuoteEnd: 18,
	}, at)
	if err != nil {
		t.Fatal(err)
	}
	rejected, err := one.Review(aid(16), domain.DecisionReject, "", "", nil, "Not useful", "A source sentence.", at)
	if err != nil {
		t.Fatal(err)
	}
	if rejected.State != domain.ProposalRejected || rejected.GeneratedStatement != one.GeneratedStatement || rejected.ReviewNote != "Not useful" {
		t.Fatalf("rejected proposal lost provenance: %+v", rejected)
	}
}

func TestProposalRetainsStructuredCandidateWithoutResolvingIdentity(t *testing.T) {
	at := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)
	one, err := domain.NewProposal(aid(21), aid(22), aid(23), aid(24), aid(25), domain.ProposalDraft{
		Statement:            "The account @eastquay posted an update.",
		Quote:                "@eastquay",
		QuoteStart:           12,
		QuoteEnd:             21,
		CandidateKind:        "account",
		CandidateName:        "@eastquay",
		CandidateDescription: "Exact account-shaped identifier; verify scope before treating it as a research record.",
	}, at)
	if err != nil {
		t.Fatal(err)
	}
	if one.CandidateKind != "account" || one.CandidateName != "@eastquay" || one.CandidateDescription == "" {
		t.Fatalf("structured candidate was not retained: %+v", one)
	}
	if _, err := domain.NewProposal(aid(26), aid(22), aid(23), aid(24), aid(25), domain.ProposalDraft{
		Statement:     "A source sentence.",
		Quote:         "A source sentence.",
		QuoteStart:    0,
		QuoteEnd:      17,
		CandidateKind: "account",
	}, at); err != domain.ErrCandidateInvalid {
		t.Fatalf("invalid structured candidate error=%v, want ErrCandidateInvalid", err)
	}
}

func TestProposalRetainsReviewedRelationshipHypothesis(t *testing.T) {
	at := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)
	one, err := domain.NewProposal(aid(31), aid(32), aid(33), aid(34), aid(35), domain.ProposalDraft{
		Statement: "East Quay is associated with @eastquay.", Quote: "East Quay", QuoteStart: 0, QuoteEnd: 9,
		CandidateKind: "place", CandidateName: "East Quay", RelationshipKind: "associated_with",
		RelatedCandidateKind: "account", RelatedCandidateName: "@eastquay", RelationshipDescription: "A reviewable hypothesis only.",
	}, at)
	if err != nil {
		t.Fatal(err)
	}
	if one.RelationshipKind != "associated_with" || one.RelatedCandidateName != "@eastquay" {
		t.Fatalf("relationship hypothesis was not retained: %+v", one)
	}
	if _, err := domain.NewProposal(aid(36), aid(32), aid(33), aid(34), aid(35), domain.ProposalDraft{
		Statement: "A source sentence.", Quote: "A source sentence.", QuoteStart: 0, QuoteEnd: 18,
		CandidateKind: "place", CandidateName: "East Quay", RelationshipKind: "located_at",
	}, at); err != domain.ErrCandidateInvalid {
		t.Fatalf("incomplete relationship error=%v, want ErrCandidateInvalid", err)
	}
}
