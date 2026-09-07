package query

import (
	"context"
	"sort"
	"time"

	"github.com/0xsj/overwatch-backend/internal/entity/domain"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

// Coverage is `CLAUDE.md`'s answerable question — *"what have I not looked
// at"* — and it is RAGGED, which is the whole of decisions/0011.
//
//	pairs = SUM over assets a of |checks applicable to kind(a)|
//
// **Never `assets × checks`.** An autonomous system number has no TLS
// certificate; that cell is not a gap in coverage, it is a question that does
// not exist. Counting it inflates the denominator and understates coverage in
// the same breath as claiming to be honest about it.
//
// This projection lives here because the root of its traversal is the ASSET —
// `0037` §4. It declares ports for `check` and `run`.

// State is a cell. FOUR, and the fourth is not a fourth kind of gap.
type State uint8

const (
	// Never: applicable, and never attempted. THE ANSWERABLE QUESTION.
	Never State = iota
	// Stale: checked, and the interval has lapsed.
	Stale
	// Fresh: checked within the check's interval — or a check with no clock,
	// which never goes stale at any age.
	Fresh
)

var stateNames = map[State]string{Never: "never", Stale: "stale", Fresh: "fresh"}

func (s State) String() string { return stateNames[s] }

// **There is no `n/a` state**, deliberately. `0011`: *"`n/a` renders as no
// square"* — it is not a pair, it appears in neither the numerator nor the
// denominator, and a fourth enum value would invite somebody to count it.
// Inapplicability is the ABSENCE of a cell.

// Check is what coverage needs to know about one check, resolved from `check`
// by the composition root. It is this package's own type: `check` is a peer.
type Check struct {
	ID        id.ID
	Name      string
	Question  string
	AppliesTo []string

	// Interval is zero for a check with no clock. `0011` requires the human
	// check to report `stale` on no input at any age, and this is why.
	Interval time.Duration

	// Human is a check with an EMPTY CHAIN. It is derived rather than flagged —
	// `0032` already made a chainless check the human one, and a second field
	// saying so is a second authority over one fact.
	Human bool
}

func (c Check) applies(kind string) bool {
	for _, k := range c.AppliesTo {
		if k == kind {
			return true
		}
	}
	return false
}

// Checks and Checked are the two ports. Neither is `check` or `run`: both are
// peers, and the composition root adapts them.
type Checks interface {
	ForCoverage(ctx context.Context, workspace id.ID) ([]Check, error)
}

type Checked interface {
	// LatestPerSubject answers the newest FINISHED invocation per (check,
	// subject). A refused invocation is not among them — the gate said no, so
	// nothing looked, and that is `never`.
	LatestPerSubject(ctx context.Context, workspace, target id.ID) ([]CheckedAt, error)
}

// CheckedAt is this package's OWN type. `run` has an identical one and is a
// peer, so the composition root translates — the same duplication `Check` above
// carries, for the same reason.
type CheckedAt struct {
	CheckID id.ID
	Kind    string
	Value   string
	At      time.Time
}

// Cell is one square. `n/a` is not representable: an inapplicable pair produces
// no Cell at all.
type Cell struct {
	CheckID   id.ID
	CheckName string
	State     State

	// At is when it was last checked, and is zero when never. A human cell's At
	// is when somebody READ it.
	At time.Time
}

// Row is one asset and its applicable checks.
type Row struct {
	Asset domain.Asset
	Cells []Cell
}

// Summary keeps the two deficits APART. `0011`: *"never and stale are different
// failures — nobody asked, versus the answer is old — and both belong in the
// summary."* The mock's own caption dropped one of them.
type Summary struct {
	Assets int
	// Pairs is the ragged denominator, and it is the claim: it asserts that
	// this many questions exist.
	Pairs int
	Fresh int
	Stale int
	Never int
}

type Report struct {
	Summary Summary
	Rows    []Row
}

// Compute assembles the grid.
//
// **An asset with no applicable checks is EXCLUDED, not 100%.** `0011` names
// this case and notes it cannot arise while the human check is universal — it is
// here anyway, because it arises immediately if that ever changes, and a row of
// zero cells rendering as complete is the worst possible way to find out.
func (g *Graph) Compute(ctx context.Context, workspace, target id.ID, now time.Time, limit int) (Report, error) {
	if workspace.IsZero() {
		return Report{}, domain.ErrWorkspaceRequired
	}
	assets, err := g.reader.Assets(ctx, workspace, target, page(limit))
	if err != nil {
		return Report{}, err
	}
	checks, err := g.checks.ForCoverage(ctx, workspace)
	if err != nil {
		return Report{}, err
	}
	checked, err := g.checked.LatestPerSubject(ctx, workspace, target)
	if err != nil {
		return Report{}, err
	}

	type key struct {
		check id.ID
		kind  string
		value string
	}
	last := make(map[key]time.Time, len(checked))
	for _, c := range checked {
		k := key{c.CheckID, c.Kind, c.Value}
		if c.At.After(last[k]) {
			last[k] = c.At
		}
	}

	out := Report{Rows: make([]Row, 0, len(assets))}
	for _, asset := range assets {
		row := Row{Asset: asset}
		for _, check := range checks {
			if !check.applies(asset.Kind) {
				// N/A — NOT A PAIR. No cell, and therefore nothing in either
				// the numerator or the denominator.
				continue
			}
			row.Cells = append(row.Cells, cell(check, asset, last[key{check.ID, asset.Kind, asset.Value}], now))
		}
		if len(row.Cells) == 0 {
			// No applicable checks at all: excluded rather than reported as
			// complete.
			continue
		}
		out.Rows = append(out.Rows, row)
		out.Summary.Assets++
		for _, c := range row.Cells {
			out.Summary.Pairs++
			switch c.State {
			case Fresh:
				out.Summary.Fresh++
			case Stale:
				out.Summary.Stale++
			default:
				out.Summary.Never++
			}
		}
	}
	return out, nil
}

// cell decides one square.
//
// The HUMAN check does not read the invocation table at all — it has no chain,
// so nothing ever spawned for it. Its cell is the fragment's READ pair, which is
// not its judgement: `0011` says READ BY YOU is what keeps `never read` and
// `no judgement` separable, and computing one from the other would be that
// collapse.
func cell(check Check, asset domain.Asset, at time.Time, now time.Time) Cell {
	out := Cell{CheckID: check.ID, CheckName: check.Name}
	if check.Human {
		if !asset.HasBeenRead() {
			return out
		}
		// A human read NEVER goes stale, at any age — 0011, and it is why the
		// interval is not consulted here.
		out.State, out.At = Fresh, asset.ReadAt
		return out
	}
	if at.IsZero() {
		return out
	}
	out.At = at
	// A check with NO CLOCK runs when somebody asks and never lapses. Zero is
	// "on demand" and not "immediately stale" — 0032 made that a kind of check
	// rather than an unset field.
	if check.Interval <= 0 || now.Sub(at) <= check.Interval {
		out.State = Fresh
		return out
	}
	out.State = Stale
	return out
}

// Percent is the ratio, and it is DELIBERATELY not a method that rounds. A
// caller that wants a percentage gets the two numbers and does its own
// arithmetic — `CLAUDE.md`: an unmeasured total renders as `–` and never as `0`,
// and a `0/0` rounded to 0% is exactly that mistake.
func (s Summary) Percent() (fresh, pairs int) { return s.Fresh, s.Pairs }

// Sort orders rows the way the asset table does: newest first, then by value, so
// the grid and the list beside it agree.
func (r Report) Sort() {
	sort.SliceStable(r.Rows, func(i, j int) bool {
		if !r.Rows[i].Asset.LastSeen.Equal(r.Rows[j].Asset.LastSeen) {
			return r.Rows[i].Asset.LastSeen.After(r.Rows[j].Asset.LastSeen)
		}
		return r.Rows[i].Asset.Value < r.Rows[j].Asset.Value
	})
}
