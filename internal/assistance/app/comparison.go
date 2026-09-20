package app

import (
	"bytes"
	"context"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/0xsj/overwatch-backend/internal/assistance/domain"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

type ComparisonDecision struct {
	LeftObservationID  id.ID
	RightObservationID id.ID
	Kind               string
	Rationale          string
}

type ComparisonInput struct {
	Observations []Observation
	Decisions    []ComparisonDecision
}

type ComparisonOutput struct {
	Status   domain.ComparisonStatus
	Text     string
	Findings []domain.ComparisonFinding
}

type ComparisonProvider interface {
	Compare(context.Context, ComparisonInput) (ComparisonOutput, error)
	Name() string
	Method() string
	TemplateVersion() string
}

// LocalComparisonProvider is intentionally conservative. It reports textual
// overlap, explicit human decisions, unique terms, and unreviewed pairs. It
// never infers a contradiction, identity, source independence, or truth from
// wording alone.
type LocalComparisonProvider struct{}

func (LocalComparisonProvider) Name() string            { return "local" }
func (LocalComparisonProvider) Method() string          { return "selected-observations-comparison-v1" }
func (LocalComparisonProvider) TemplateVersion() string { return "comparison-v1" }

var comparisonTokenPattern = regexp.MustCompile(`(?i)[\p{L}\p{N}][\p{L}\p{N}@._'’-]{1,63}`)

var comparisonStopWords = map[string]struct{}{
	"about": {}, "after": {}, "again": {}, "also": {}, "among": {}, "because": {}, "before": {}, "being": {},
	"between": {}, "could": {}, "from": {}, "have": {}, "into": {}, "more": {}, "other": {}, "same": {},
	"some": {}, "than": {}, "that": {}, "their": {}, "there": {}, "these": {}, "they": {}, "this": {},
	"through": {}, "under": {}, "were": {}, "which": {}, "with": {}, "would": {}, "your": {},
}

func (LocalComparisonProvider) Compare(ctx context.Context, input ComparisonInput) (ComparisonOutput, error) {
	if err := ctx.Err(); err != nil {
		return ComparisonOutput{}, err
	}
	findings := make([]domain.ComparisonFinding, 0)
	termSets := make([]map[string]struct{}, len(input.Observations))
	for index, observation := range input.Observations {
		termSets[index] = comparisonTerms(observation.Statement + "\n" + observation.Quote)
	}
	decisions := make(map[string]ComparisonDecision, len(input.Decisions))
	for _, decision := range input.Decisions {
		decisions[comparisonPairKey(decision.LeftObservationID, decision.RightObservationID)] = decision
	}

	for left := 0; left < len(input.Observations); left++ {
		for right := left + 1; right < len(input.Observations); right++ {
			leftObservation, rightObservation := input.Observations[left], input.Observations[right]
			shared := comparisonIntersection(termSets[left], termSets[right])
			pairIDs := []id.ID{leftObservation.ID, rightObservation.ID}
			if len(shared) >= 2 {
				findings = append(findings, domain.ComparisonFinding{Kind: domain.Agreement, Summary: "The selected passages share textual details: " + strings.Join(shared, ", ") + ". Review the exact citations before treating this as agreement.", ObservationIDs: pairIDs})
			}
			minimum := minInt(len(termSets[left]), len(termSets[right]))
			if len(shared) >= 3 && minimum > 0 && float64(len(shared))/float64(minimum) >= 0.7 {
				findings = append(findings, domain.ComparisonFinding{Kind: domain.PossibleRepetition, Summary: "The selected passages have high textual overlap and may reflect repeated reporting; inspect their source chain and exact wording.", ObservationIDs: pairIDs})
			}
			decision, reviewed := decisions[comparisonPairKey(leftObservation.ID, rightObservation.ID)]
			if reviewed {
				switch strings.TrimSpace(decision.Kind) {
				case "supports":
					findings = append(findings, domain.ComparisonFinding{Kind: domain.Agreement, Summary: "Human review marked this pair as supporting: " + strings.TrimSpace(decision.Rationale), ObservationIDs: pairIDs})
				case "contradicts":
					findings = append(findings, domain.ComparisonFinding{Kind: domain.Contradiction, Summary: "Human review marked this pair as contradicting: " + strings.TrimSpace(decision.Rationale), ObservationIDs: pairIDs})
				case "repeats":
					findings = append(findings, domain.ComparisonFinding{Kind: domain.PossibleRepetition, Summary: "Human review marked this pair as repeating: " + strings.TrimSpace(decision.Rationale), ObservationIDs: pairIDs})
				case "unresolved":
					findings = append(findings, domain.ComparisonFinding{Kind: domain.CoverageGap, Summary: "Human review leaves this pair unresolved: " + strings.TrimSpace(decision.Rationale), ObservationIDs: pairIDs})
				}
			} else {
				findings = append(findings, domain.ComparisonFinding{Kind: domain.CoverageGap, Summary: "No human pair decision is saved for these selected observations.", ObservationIDs: pairIDs})
			}
		}
	}

	for index, observation := range input.Observations {
		unique := make([]string, 0)
		for term := range termSets[index] {
			seenElsewhere := false
			for other, terms := range termSets {
				if other != index {
					if _, exists := terms[term]; exists {
						seenElsewhere = true
						break
					}
				}
			}
			if !seenElsewhere {
				unique = append(unique, term)
			}
		}
		sort.Strings(unique)
		if len(unique) > 5 {
			unique = unique[:5]
		}
		if len(unique) > 0 {
			findings = append(findings, domain.ComparisonFinding{Kind: domain.UniqueDetail, Summary: "Only this selected observation contains these distinctive terms: " + strings.Join(unique, ", ") + ". Verify whether they are material details or incidental wording.", ObservationIDs: []id.ID{observation.ID}})
		}
	}

	if len(findings) > domain.MaxComparisonFindings {
		findings = findings[:domain.MaxComparisonFindings]
	}
	status := domain.ComparisonCompleted
	if len(findings) == 0 {
		status = domain.ComparisonEmpty
	}
	return ComparisonOutput{Status: status, Text: comparisonText(input.Observations, findings), Findings: findings}, nil
}

func comparisonTerms(text string) map[string]struct{} {
	terms := make(map[string]struct{})
	for _, raw := range comparisonTokenPattern.FindAllString(text, -1) {
		term := strings.ToLower(strings.Trim(raw, "-_.’'"))
		if len([]rune(term)) < 3 {
			continue
		}
		if _, stop := comparisonStopWords[term]; stop {
			continue
		}
		terms[term] = struct{}{}
	}
	return terms
}

func comparisonIntersection(left, right map[string]struct{}) []string {
	shared := make([]string, 0)
	for term := range left {
		if _, exists := right[term]; exists {
			shared = append(shared, term)
		}
	}
	sort.Strings(shared)
	if len(shared) > 5 {
		shared = shared[:5]
	}
	return shared
}

func comparisonPairKey(left, right id.ID) string {
	if bytes.Compare(left[:], right[:]) > 0 {
		left, right = right, left
	}
	return left.String() + ":" + right.String()
}

func comparisonText(observations []Observation, findings []domain.ComparisonFinding) string {
	lines := []string{"Assisted comparison proposal", "", "Selected observations: " + intString(len(observations)), ""}
	if len(findings) == 0 {
		return strings.Join(append(lines, "No structured comparison findings were generated."), "\n")
	}
	for _, kind := range []domain.FindingKind{domain.Agreement, domain.Contradiction, domain.PossibleRepetition, domain.UniqueDetail, domain.CoverageGap} {
		count := 0
		for _, finding := range findings {
			if finding.Kind == kind {
				count++
			}
		}
		if count == 0 {
			continue
		}
		lines = append(lines, strings.ReplaceAll(kind.String(), "_", " ")+":")
		for _, finding := range findings {
			if finding.Kind != kind {
				continue
			}
			lines = append(lines, "- "+finding.Summary+" ["+shortObservationIDs(finding.ObservationIDs)+"]")
		}
		lines = append(lines, "")
	}
	return strings.Join(lines, "\n")
}

func shortObservationIDs(ids []id.ID) string {
	short := make([]string, 0, len(ids))
	for _, one := range ids {
		raw := one.String()
		if len(raw) > 8 {
			raw = raw[:8]
		}
		short = append(short, raw)
	}
	return strings.Join(short, ", ")
}

func intString(value int) string {
	return strconv.Itoa(value)
}

func minInt(left, right int) int {
	if left < right {
		return left
	}
	return right
}
