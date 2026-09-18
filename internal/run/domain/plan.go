package domain

import (
	"strings"

	"github.com/0xsj/overwatch-backend/pkg/id"
)

// Step is what the planner needs to know about one node of a chain, resolved
// from `check` and `tool` by the command. It is run's OWN type, because those
// two are peers this package may not import.
type Step struct {
	StepID id.ID
	ToolID id.ID

	// Template is `tool.argv` as stored: text, with placeholders.
	Template string

	// Source says nothing feeds this step, so it is seeded from the target
	// rather than from another tool's output.
	Source bool

	// Loud is the tool's intensity being `loud`, carried separately because the
	// question it answers is a different one: whether the run needs `admin`.
	Loud bool

	// Intensity is the tool's, spelled — and it is what the SPAWN GATE
	// qualifies. `0010`: "a range in scope for passive collection is not
	// thereby in scope for a loud scan." Omitting it made a rule permitting only
	// `passive` permit everything, which is a hole in the scope model rather
	// than a rough edge.
	Intensity string

	// Kind is the scope vocabulary's name for what this step would touch, or
	// EMPTY when there is no translation. Empty fails closed — see [Slot].
	//
	// At a SOURCE step it is the tool's `produces` and at a downstream one its
	// `consumes`, which is also the filter applied to the upstream observations
	// — decisions/0039 §3. One field because it is one question: what does this
	// step deal in.
	Kind string

	// Upstream is the steps that FEED this one, so a downstream step's
	// candidates can be read from their invocations' observations. It is the
	// chain's edges carried into run's vocabulary, and it is empty exactly when
	// [Step.Source] is true.
	Upstream []id.ID
}

// Slot is one planned invocation before the spawn gate has been asked.
type Slot struct {
	Step Step
	Argv []string

	// Values is what this step is aimed at, and it is EMPTY on a downstream
	// step — decisions/0039 §3. A source step's one candidate is the target and
	// is known at plan time; a downstream step's are the subjects its feeders
	// observed, which nothing knows until they have run.
	Values []string

	// Intensity travels with the slot because the gate needs it and the slot is
	// what the gate is asked about.
	Intensity string
}

// SkipNothingUpstream is what a downstream step whose feeders produced nothing
// records. It used to say the same thing for a different reason — nothing could
// EVER feed it, because `observation` was unbuilt — and decisions/0039 §3 is
// the record that made it mean what it says.
const SkipNothingUpstream = "nothing upstream produced observations to feed it"

// Outline turns an ordered chain into planned slots. It does not ask the gate,
// does not mint ids and does not touch a clock — everything here is a pure
// function of the chain and the target, which is what makes the plan and the
// client's spawn PREVIEW the same computation.
func Outline(steps []Step, target string) ([]Slot, error) {
	if len(steps) == 0 {
		return nil, ErrChainEmpty
	}
	if strings.TrimSpace(target) == "" {
		return nil, ErrTargetRequired
	}
	out := make([]Slot, 0, len(steps))
	for _, s := range steps {
		slot := Slot{Step: s, Intensity: s.Intensity}
		if s.Source {
			// ONE CANDIDATE: the target. Today's behaviour is the
			// one-candidate case of the general shape — decisions/0039 §2.
			slot.Values = []string{Fold(target)}
		}
		// A downstream step gets the SPLIT, UNSUBSTITUTED template: there is
		// nothing to substitute yet, and writing it this way keeps the argv
		// non-empty from the first insert while staying visibly unresolved —
		// 0039 §4.
		argv, err := Argv(s.Template, slot.Values...)
		if err != nil {
			return nil, err
		}
		slot.Argv = argv
		out = append(out, slot)
	}
	return out, nil
}

// Argv resolves a template into the slice execx spawns.
//
// **SPLIT FIRST, THEN SUBSTITUTE.** That order is the entire safety property and
// it is the reason this is a function rather than a line in the caller:
//
//	fields, then substitute   ["subfinder" "-d" "x; rm -rf /"]   ONE element
//	substitute, then fields   ["subfinder" "-d" "x;" "rm" ...]   FOUR
//
// A target's name is attacker-influenced by definition. Splitting first means a
// substituted value is exactly one argv element whatever is in it, and there is
// no escaping to get wrong — which is the same property `pkg/execx` has for
// taking a slice at all.
//
// Any `{{...}}` placeholder takes the value, so a template reads naturally
// whether it says `{{target}}`, `{{domain}}` or `{{host}}`. Distinguishing them
// would be a binding vocabulary, and there is nothing yet to bind: a step has
// one input kind — decisions/0039 §3.
//
// **A placeholder therefore cannot contain whitespace** — the split has already
// happened by the time anything looks for `{{`, so `{{two words}}` is two fields
// and neither resolves. That is the price of the order above and it is worth it;
// there is a test that pins it so it is a documented limit rather than a
// surprise.
//
// # Many values, and none
//
// decisions/0039 made a step one invocation over MANY candidates and did not say
// how many values become one command line. Two encodings were available and the
// choice is here rather than in the record because it is an argv question:
//
//	EXPAND IN PLACE   `-u {{host}}` x3  ->  ["-u" "a" "b" "c"]
//	JOIN WITH COMMAS  `-u {{host}}` x3  ->  ["-u" "a,b,c"]
//
// **Expand in place**, because it is the exact generalisation of the safety
// property above: one value is one element, so N values are N elements, and no
// character inside a value can ever be read as a separator. Comma-joining is
// what `httpx -u` and `nuclei -u` actually accept, and it was refused because a
// value containing a comma silently becomes two targets — a tool-level
// confusion that nothing in the record would show.
//
// The cost is real and it is VISIBLE: `httpx -u a b c` is not a command httpx
// understands, and the argv is stored verbatim and rendered, so a wrong template
// is read off the run rather than inferred from a bad result. The right fix is a
// list file — which is the shape 0039 §1 quotes — and it is owed.
//
// **With NO values the template is split and returned unsubstituted**, which is
// how a downstream step is planned: `{{host}}` stays `{{host}}` until something
// upstream has found a host. That is 0039 §4, and it is one function rather than
// two so the planned argv and the run one cannot be built by different code.
func Argv(template string, values ...string) ([]string, error) {
	fields := strings.Fields(template)
	if len(fields) == 0 {
		return nil, ErrArgvEmpty
	}
	return Fill(fields, values), nil
}

// Fill substitutes into fields that are ALREADY SPLIT. It exists because a
// downstream invocation is planned with the split template and resolved when it
// runs — decisions/0039 Section 4 — and re-reading the tool's template at that
// moment would let a tool edited mid-run change what the plan said would happen.
//
// It is the same walk [Argv] does after `strings.Fields`, so the planned argv
// and the run one cannot be built by two different pieces of code.
func Fill(fields []string, values []string) []string {
	out := make([]string, 0, len(fields))
	for _, field := range fields {
		switch {
		case len(values) == 0 || !strings.Contains(field, "{{"):
			// No values, or nothing to put them in. Either way the field is
			// carried through EXACTLY as written — a template with no
			// placeholder is a flag, and a template with one and nothing to
			// substitute is an honest record of what would have run.
			out = append(out, field)
		default:
			for _, value := range values {
				out = append(out, substitute(field, value))
			}
		}
	}
	return out
}

// substitute replaces every `{{…}}` in ONE already-split field. It walks rather
// than using a regexp because the grammar is two characters and because a
// regexp here would be the kind of thing somebody later "improves" into
// accepting a shell.
func substitute(field, value string) string {
	var b strings.Builder
	for {
		open := strings.Index(field, "{{")
		if open < 0 {
			b.WriteString(field)
			return b.String()
		}
		close := strings.Index(field[open:], "}}")
		if close < 0 {
			// An unterminated placeholder is left EXACTLY as written. Guessing
			// at the intent would put `{{targe` into an argv as if it had been
			// meant, and the tool's own error is a better message than ours.
			b.WriteString(field)
			return b.String()
		}
		b.WriteString(field[:open])
		b.WriteString(value)
		field = field[open+close+2:]
	}
}

// Gate is the spawn gate's answer in run's own vocabulary — decisions/0010,
// asked through a port because `scope` is a peer.
//
// **A refusal with NO RULE is a different fact from one with a rule.** Nothing
// permitted this, versus a rule excluded it. `0010`'s "nothing is in scope until
// a rule says so" makes the empty case a refusal, and the record keeps them
// apart because a client's report cites the rule when there is one.
type Gate struct {
	Permitted bool
	Rule      id.ID
	Reason    string
}
