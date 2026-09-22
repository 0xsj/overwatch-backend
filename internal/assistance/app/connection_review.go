package app

import (
	"context"
	"strings"

	"github.com/0xsj/overwatch-backend/internal/assistance/domain"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

type ConnectionReviewInput struct {
	ConnectionID             id.ID
	FromRecordID             id.ID
	ToRecordID               id.ID
	ConnectionKind           string
	ConnectionState          string
	ConnectionRationale      string
	SupportingObservationIDs []id.ID
	OpposingObservationIDs   []id.ID
	SupportingObservations   []Observation
	OpposingObservations     []Observation
	Decisions                []ComparisonDecision
}

type ConnectionReviewOutput struct {
	Status   domain.ConnectionReviewStatus
	Text     string
	Findings []domain.ConnectionReviewFinding
}

type ConnectionReviewProvider interface {
	ReviewConnection(context.Context, ConnectionReviewInput) (ConnectionReviewOutput, error)
	Name() string
	Method() string
	TemplateVersion() string
}

// LocalConnectionReviewProvider turns the authored evidence split into a
// bounded review prompt. It does not alter the connection or infer identity,
// causality, source independence, or truth.
type LocalConnectionReviewProvider struct{}

func (LocalConnectionReviewProvider) Name() string            { return "local" }
func (LocalConnectionReviewProvider) Method() string          { return "authored-connection-review-v1" }
func (LocalConnectionReviewProvider) TemplateVersion() string { return "connection-review-v1" }
func (LocalConnectionReviewProvider) External() bool          { return false }

func (LocalConnectionReviewProvider) ReviewConnection(ctx context.Context, input ConnectionReviewInput) (ConnectionReviewOutput, error) {
	if err := ctx.Err(); err != nil {
		return ConnectionReviewOutput{}, err
	}
	selected := append(append([]id.ID{}, input.SupportingObservationIDs...), input.OpposingObservationIDs...)
	if len(selected) == 0 {
		return ConnectionReviewOutput{Status: domain.ConnectionReviewEmpty, Text: "Assisted connection review proposal\n\nNo supporting or opposing observations were attached to the authored connection.", Findings: []domain.ConnectionReviewFinding{}}, nil
	}
	findings := make([]domain.ConnectionReviewFinding, 0, 4)
	if len(input.SupportingObservationIDs) > 0 {
		findings = append(findings, domain.ConnectionReviewFinding{
			Kind:           domain.ConnectionSupport,
			Summary:        "The authored assessment labels these citations as supporting. Review the exact passages and their source chain before treating the relationship as strengthened.",
			ObservationIDs: append([]id.ID(nil), input.SupportingObservationIDs...),
		})
	}
	if len(input.OpposingObservationIDs) > 0 {
		findings = append(findings, domain.ConnectionReviewFinding{
			Kind:           domain.ConnectionOpposition,
			Summary:        "The authored assessment labels these citations as opposing. Keep the relationship qualified until the tension is explained or resolved by further review.",
			ObservationIDs: append([]id.ID(nil), input.OpposingObservationIDs...),
		})
	}
	for _, decision := range input.Decisions {
		kind := domain.ConnectionSupport
		prefix := "Human review marked the cited pair as supporting: "
		if strings.TrimSpace(decision.Kind) == "contradicts" {
			kind = domain.ConnectionOpposition
			prefix = "Human review marked the cited pair as contradicting: "
		} else if strings.TrimSpace(decision.Kind) != "supports" {
			continue
		}
		findings = append(findings, domain.ConnectionReviewFinding{Kind: kind, Summary: prefix + strings.TrimSpace(decision.Rationale), ObservationIDs: []id.ID{decision.LeftObservationID, decision.RightObservationID}})
	}
	findings = append(findings,
		domain.ConnectionReviewFinding{
			Kind:           domain.ConnectionAlternative,
			Summary:        "Alternative possibility to test: the two records may be related through a shared event, source, location, or reporting chain without establishing the authored relationship as the only explanation.",
			ObservationIDs: append([]id.ID(nil), selected...),
		},
		domain.ConnectionReviewFinding{
			Kind:           domain.ConnectionDiscriminatingEvidence,
			Summary:        "Discriminating evidence to seek: an independent observation that directly tests the authored rationale and can explain both the supporting and opposing citations. The current citations are a review basis, not proof that the relationship is true.",
			ObservationIDs: append([]id.ID(nil), selected...),
		},
	)
	if len(findings) > domain.MaxConnectionReviewFindings {
		findings = findings[:domain.MaxConnectionReviewFindings]
	}
	lines := []string{"Assisted connection review proposal", "", "Authored relationship: " + input.ConnectionKind + " (" + input.ConnectionState + ")", "", "Review findings:"}
	for _, finding := range findings {
		lines = append(lines, "- "+strings.ReplaceAll(finding.Kind.String(), "_", " ")+": "+finding.Summary)
	}
	return ConnectionReviewOutput{Status: domain.ConnectionReviewCompleted, Text: strings.Join(lines, "\n"), Findings: findings}, nil
}
