package app

import (
	"context"
	"regexp"
	"sort"
	"strings"

	"github.com/0xsj/overwatch-backend/internal/assistance/domain"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

type Observation struct {
	ID          id.ID
	WorkspaceID id.ID
	SourceTitle string
	Statement   string
	Quote       string
}

type SynthesisOutput struct {
	Text       string
	Candidates []domain.SynthesisCandidate
}

type SynthesisProvider interface {
	Synthesize(context.Context, []Observation) (SynthesisOutput, error)
	Name() string
	Method() string
}

// LocalSynthesisProvider preserves the current browser review pass as a
// deterministic backend operation. It only formats selected statements and
// recognizes exact account-shaped identifiers; it does not infer identity,
// agreement, contradiction, or relationships.
type LocalSynthesisProvider struct{}

func (LocalSynthesisProvider) Name() string   { return "local" }
func (LocalSynthesisProvider) Method() string { return "selected-observations-v1" }

var (
	localEmailPattern  = regexp.MustCompile(`(?i)\b[a-z0-9][a-z0-9.!#$%&'*+/=?^_\x60{|}~-]{0,63}@[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?(?:\.[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?)+\b`)
	localHandlePattern = regexp.MustCompile(`(?i)@[a-z0-9][a-z0-9_.-]{1,63}`)
)

func (LocalSynthesisProvider) Synthesize(ctx context.Context, observations []Observation) (SynthesisOutput, error) {
	if err := ctx.Err(); err != nil {
		return SynthesisOutput{}, err
	}
	lines := make([]string, 0, len(observations))
	found := make(map[string]domain.SynthesisCandidate)
	for _, observation := range observations {
		lines = append(lines, observation.SourceTitle+": "+observation.Statement)
		text := observation.Statement + "\n" + observation.Quote
		emails := localEmailPattern.FindAllStringIndex(text, -1)
		for _, match := range emails {
			localAddCandidate(found, text[match[0]:match[1]], observation.ID)
		}
		for _, match := range localHandlePattern.FindAllStringIndex(text, -1) {
			insideEmail := false
			for _, email := range emails {
				if match[0] >= email[0] && match[0] < email[1] {
					insideEmail = true
					break
				}
			}
			if !insideEmail {
				localAddCandidate(found, text[match[0]:match[1]], observation.ID)
			}
		}
	}
	candidates := make([]domain.SynthesisCandidate, 0, len(found))
	for _, candidate := range found {
		candidates = append(candidates, candidate)
	}
	sort.Slice(candidates, func(i, j int) bool { return strings.ToLower(candidates[i].Name) < strings.ToLower(candidates[j].Name) })
	return SynthesisOutput{Text: strings.Join(lines, "\n"), Candidates: candidates}, nil
}

func localAddCandidate(found map[string]domain.SynthesisCandidate, name string, observation id.ID) {
	clean := strings.TrimSpace(name)
	key := strings.ToLower(clean)
	if existing, ok := found[key]; ok {
		for _, one := range existing.ObservationIDs {
			if one == observation {
				return
			}
		}
		existing.ObservationIDs = append(existing.ObservationIDs, observation)
		found[key] = existing
		return
	}
	found[key] = domain.SynthesisCandidate{Kind: "account", Name: clean, ObservationIDs: []id.ID{observation}, Rationale: "Exact account-shaped identifier surfaced in selected retained observations; verify what the identifier refers to before saving."}
}
