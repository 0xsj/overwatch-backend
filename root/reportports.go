package root

import (
	"context"
	"strings"
	"time"

	entquery "github.com/0xsj/overwatch-backend/internal/entity/app/query"
	findingquery "github.com/0xsj/overwatch-backend/internal/finding/app/query"
	findingdomain "github.com/0xsj/overwatch-backend/internal/finding/domain"
	reportdomain "github.com/0xsj/overwatch-backend/internal/report/domain"
	runquery "github.com/0xsj/overwatch-backend/internal/run/app/query"
	scopequery "github.com/0xsj/overwatch-backend/internal/scope/app/query"
	scopedomain "github.com/0xsj/overwatch-backend/internal/scope/domain"
	"github.com/0xsj/overwatch-backend/pkg/clock"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

// This file is where `report` meets the five domains it may not import.
//
// **A section is a capability** — `CLAUDE.md` — and this file is where that
// stops being a slogan: each method asks the domain that OWNS a number for it,
// and a section nobody can source has no adapter and therefore cannot exist.
type sections struct {
	rules    *scopequery.Rules
	graph    *entquery.Graph
	findings *findingquery.Findings
	runs     *runquery.Runs
	clock    clock.System

	// engagementNotes is embedded rather than a field, so `sections` satisfies
	// the eighth method without this file knowing anything about `note`.
	engagementNotes
}

// Scope is the METHOD section. A SUPERSEDED rule is still cited: `0030` keeps it
// forever precisely so a citation made at the time still resolves, and a report
// that hid it would break the only reason it was kept.
func (s sections) Scope(ctx context.Context, workspace, target id.ID) ([]reportdomain.Rule, error) {
	found, err := s.rules.All(ctx, workspace, target)
	if err != nil {
		return nil, err
	}
	out := make([]reportdomain.Rule, 0, len(found))
	for _, one := range found {
		out = append(out, reportdomain.Rule{
			Pattern: one.Pattern, Polarity: one.Polarity.String(),
			Gate: one.Gate.String(), Intensity: intensities(one.Tools),
			Superseded: !one.SupersededBy.IsZero(),
		})
	}
	return out, nil
}

// Attribution is why each asset is thought to be theirs. It walks the asset view
// rather than every fragment, because an attribution on a fragment nobody
// attributed to this target is another engagement's evidence.
func (s sections) Attribution(ctx context.Context, workspace, target id.ID) ([]reportdomain.Claim, error) {
	assets, err := s.graph.AllAssets(ctx, workspace, target)
	if err != nil {
		return nil, err
	}
	out := make([]reportdomain.Claim, 0, len(assets))
	for _, one := range assets {
		claim := reportdomain.Claim{
			Fragment: one.Value, Kind: one.Kind,
			Claimant: one.Claimant.String(), Basis: one.Basis,
			// An asset EXISTS because an accepted attribution does — 0009's
			// predicate — so the state is not a variable here.
			State: "accepted",
		}
		// CONFIDENCE IS DELIBERATELY ABSENT HERE. `entity.Asset` carries the
		// claimant and the basis but not the number, and only a MODEL has one —
		// 0003 and 0004. Reading a zero off a struct that does not hold it and
		// rendering it would make a rule's category look like a probability of
		// zero, which is the exact collapse those two records exist to prevent.
		// The day a model proposes an attribution, the asset view gains the
		// field and this line changes; inventing it now would be worse than
		// omitting it.
		out = append(out, claim)
	}
	return out, nil
}

func (s sections) Assets(ctx context.Context, workspace, target id.ID) ([]reportdomain.Asset, error) {
	found, err := s.graph.AllAssets(ctx, workspace, target)
	if err != nil {
		return nil, err
	}
	out := make([]reportdomain.Asset, 0, len(found))
	for _, one := range found {
		out = append(out, reportdomain.Asset{
			Kind: one.Kind, Value: one.Value,
			// EVERY row of the asset view is in scope by construction: an asset
			// IS a fragment carrying an accepted attribution, and 0036 makes the
			// claim gate what produces one. The field is here because the
			// report's count partitions on it, and the day a fragment appears
			// here without a claim this must stop being a constant.
			InScope:  true,
			LastSeen: date(one.LastSeen),
		})
	}
	return out, nil
}

// Findings excludes DISMISSED ones from the rows and counts them separately.
// *"8 open · 1 dismissed and excluded"* is one sentence saying both; a silently
// shorter list says neither.
func (s sections) Findings(ctx context.Context, workspace, target id.ID) ([]reportdomain.Finding, int, error) {
	fragments, err := s.graph.AcceptedFragmentsForTarget(ctx, workspace, target)
	if err != nil {
		return nil, 0, err
	}
	found, err := s.findings.ForFragments(ctx, workspace, fragments)
	if err != nil {
		return nil, 0, err
	}
	out := make([]reportdomain.Finding, 0, len(found))
	dismissed := 0
	for _, one := range found {
		if one.State == findingdomain.StateDismissed {
			dismissed++
			continue
		}
		out = append(out, reportdomain.Finding{
			Signature: one.Signature, Severity: one.Severity.String(),
			State: one.State.String(), Fragment: one.FragmentValue,
			Sightings: one.Sightings, LastSeen: date(one.LastSeen),
		})
	}
	return out, dismissed, nil
}

func (s sections) Coverage(ctx context.Context, workspace, target id.ID) ([]reportdomain.Cell, error) {
	grid, err := s.graph.ComputeAll(ctx, workspace, target, s.clock.Now())
	if err != nil {
		return nil, err
	}
	out := make([]reportdomain.Cell, 0, len(grid.Rows))
	for _, row := range grid.Rows {
		for _, cell := range row.Cells {
			out = append(out, reportdomain.Cell{
				Asset: row.Asset.Value, Check: cell.CheckName,
				State: cell.State.String(), At: date(cell.At),
			})
		}
	}
	return out, nil
}

// Invocations is the SCOPE PROOF — the record of what did NOT run. It walks
// every run of this target, because a report covering an engagement covers all
// of it and a page limit here would silently truncate the proof.
func (s sections) Invocations(ctx context.Context, workspace, target id.ID) ([]reportdomain.Invocation, error) {
	runs, err := s.runs.All(ctx, workspace, target)
	if err != nil {
		return nil, err
	}
	out := []reportdomain.Invocation{}
	for _, one := range runs {
		detail, err := s.runs.Detail(ctx, workspace, one.ID)
		if err != nil {
			return nil, err
		}
		for _, inv := range detail.Invocations {
			held := reportdomain.Invocation{
				Tool: inv.ToolID.String(), State: inv.Phase.String(),
				Argv: inv.Argv, Refusal: inv.RefusalReason,
			}
			if !inv.RefusalRule.IsZero() {
				held.RefusalRule = inv.RefusalRule.String()
			}
			// THE PER-CANDIDATE PROOF — 0039. A step can be `ok` while a rule
			// kept it off part of what it was pointed at, and a report omitting
			// that under-reports every batched run.
			for _, c := range detail.Candidates[inv.ID] {
				if c.Permitted {
					held.Permitted++
				} else {
					held.Refused++
				}
			}
			out = append(out, held)
		}
	}
	return out, nil
}

// Artifacts is the REPLAY appendix. The bytes are not here — the hash is, which
// is what a reader re-derives from.
func (s sections) Artifacts(ctx context.Context, workspace, target id.ID) ([]reportdomain.Artifact, error) {
	runs, err := s.runs.All(ctx, workspace, target)
	if err != nil {
		return nil, err
	}
	out := []reportdomain.Artifact{}
	for _, one := range runs {
		detail, err := s.runs.Detail(ctx, workspace, one.ID)
		if err != nil {
			return nil, err
		}
		for _, inv := range detail.Invocations {
			for _, a := range detail.Artifacts[inv.ID] {
				out = append(out, reportdomain.Artifact{
					Stream: a.Stream.String(), Hash: a.Hash, Bytes: a.Bytes,
					MediaType: a.MediaType, Truncated: a.Truncated,
				})
			}
		}
	}
	return out, nil
}

// intensities spells a spawn rule's tool list. `0010` qualifies a spawn rule by
// the intensities it permits — *"a range in scope for passive collection is not
// thereby in scope for a loud scan"* — and a method section that omitted it
// would overstate what the engagement authorised.
//
// EMPTY on a claim rule, which is correct: there are no processes on that gate.
func intensities(tools []scopedomain.Intensity) string {
	if len(tools) == 0 {
		return ""
	}
	names := make([]string, 0, len(tools))
	for _, one := range tools {
		names = append(names, one.String())
	}
	return strings.Join(names, ", ")
}

func date(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}
