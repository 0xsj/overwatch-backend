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
	Kind string
}

// Slot is one planned invocation before the spawn gate has been asked. `Skip`
// non-empty means the gate is not asked at all: there is nothing to ask about,
// because this step will not run today.
type Slot struct {
	Step Step
	Argv []string

	// Value is what the argv was built around — the target's name for a source
	// step. It is what the gate is asked about.
	Value string

	// Intensity travels with the slot because the gate needs it and the slot is
	// what the gate is asked about.
	Intensity string

	// Skip names what did not arrive. It is the honest state of every
	// downstream step until `observation` exists: feeding step two needs to
	// know what step one FOUND, and nothing parses output yet.
	Skip string
}

// SkipNoObservations is the reason every non-source step carries today. It
// names the missing noun rather than apologising, so the day `observation`
// lands the string disappears from the record instead of becoming a lie.
const SkipNoObservations = "nothing upstream produced observations to feed it"

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
		slot := Slot{Step: s, Value: target, Intensity: s.Intensity}
		argv, err := Argv(s.Template, target)
		if err != nil {
			return nil, err
		}
		slot.Argv = argv
		if !s.Source {
			slot.Skip = SkipNoObservations
		}
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
// would be a binding vocabulary, and there is nothing yet to bind: a source step
// has exactly one input.
//
// **A placeholder therefore cannot contain whitespace** — the split has already
// happened by the time anything looks for `{{`, so `{{two words}}` is two fields
// and neither resolves. That is the price of the order above and it is worth it;
// there is a test that pins it so it is a documented limit rather than a
// surprise.
func Argv(template, value string) ([]string, error) {
	fields := strings.Fields(template)
	if len(fields) == 0 {
		return nil, ErrArgvEmpty
	}
	out := make([]string, 0, len(fields))
	for _, field := range fields {
		out = append(out, substitute(field, value))
	}
	return out, nil
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
