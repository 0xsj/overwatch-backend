package domain

import (
	"sort"
	"time"

	"github.com/0xsj/overwatch-backend/pkg/id"
)

// Pair is one (target, check) the scheduler could start a run for, with
// everything needed to decide whether it should. It is resolved from `check`,
// `workspace` and `target` by the composition root — all peers.
type Pair struct {
	WorkspaceID id.ID
	TargetID    id.ID
	CheckID     id.ID
	CheckName   string

	// Interval is how often this question gets asked. ZERO means the check runs
	// when somebody asks and never on a clock — decisions/0032 made that a kind
	// of check rather than an unset field, and it is why this is never due.
	Interval time.Duration

	// Enabled is the standing AUTHORISATION — decisions/0038. A scheduled run is
	// not authorised when it starts, because nobody is there; it is authorised
	// when somebody with `owner` or `admin` wrote the interval and turned this
	// on. Disabling is how that is withdrawn.
	Enabled bool

	// Human is `READ BY YOU`. Nothing spawns for it — a person reading the thing
	// is the whole act — so it is never due however long it has been.
	Human bool

	// Runnable says the check HAS A CHAIN. A chainless non-human check is
	// enabled, has an interval, and cannot run — and because a failed start
	// never updates `last_started`, retrying it forever leaves it at the head of
	// the queue consuming a slot every tick. The first live tick showed exactly
	// that: two unfinished checks starved everything behind them.
	//
	// It is the same distinction `0037` drew between `human` and `unfinished`,
	// arriving a second time in a different place.
	HasChain bool
}

// PairKey is what "the last run of this check against this target" is keyed by.
// The clock is PER PAIR: a firm running one check over sixty clients is sixty
// independent clocks, and a shared one would mean fifty-nine engagements are
// covered because the sixtieth was scanned.
type PairKey struct {
	TargetID id.ID
	CheckID  id.ID
}

func (p Pair) Key() PairKey { return PairKey{TargetID: p.TargetID, CheckID: p.CheckID} }

// Runnable is whether this pair is ever a candidate, independent of the clock.
// Three reasons a pair is never due, and each is a different fact:
//
//	!Enabled     the standing authorisation is withdrawn
//	Human        nothing spawns; a person reading it is the whole act
//	Interval 0   it runs when somebody asks, not on a clock
//	!HasChain    it is unfinished — nothing to run, and retrying starves
func (p Pair) Runnable() bool {
	return p.Enabled && !p.Human && p.Interval > 0 && p.HasChain
}

// Due answers which pairs should start a run now.
//
// **It is a pure function** — decisions/0038 §2. No clock, no store, no ports,
// so the entire scheduling rule is testable without a database or a process,
// which is the part of a scheduler that is normally only observable by waiting.
//
// `last` is the newest START of each pair. A pair absent from it has never run,
// which is due — that is what makes a newly created check take effect against
// targets that already existed.
//
// The OLDEST are taken first when the cap bites, so a large estate makes
// progress round-robin rather than starving whatever sorts last.
func Due(pairs []Pair, last map[PairKey]time.Time, now time.Time, limit int) []Pair {
	out := make([]Pair, 0, len(pairs))
	for _, p := range pairs {
		if !p.Runnable() {
			continue
		}
		at, ran := last[p.Key()]
		if ran && now.Sub(at) < p.Interval {
			continue
		}
		out = append(out, p)
	}

	sort.SliceStable(out, func(i, j int) bool {
		ai, oki := last[out[i].Key()]
		aj, okj := last[out[j].Key()]
		switch {
		case !oki && !okj:
			// Both have never run. Stable order, so a fixed input produces a
			// fixed plan — a scheduler whose output reshuffles is one nobody can
			// reason about from a log.
			return false
		case !oki:
			// Never run beats long ago: `never` is the deficit 0011 calls the
			// answerable question, and it is the one worth closing first.
			return true
		case !okj:
			return false
		default:
			return ai.Before(aj)
		}
	})

	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}
