// Package run is a pipeline against a target, and the record of every process
// inside it.
//
// Three nouns, one domain — decisions/0033:
//
//	run          workspace · target · check · state · started_by
//	invocation   one process: argv, exit, bytes — or a refusal
//	artifact     the bytes, verbatim and content-addressed
//
// # The plan is written before anything spawns
//
// Every step of the check's chain gets an invocation row up front, and the
// SPAWN GATE IS ASKED AT PLAN TIME. A step the gate refuses is a `refused`
// invocation that never had a process, carrying the id of the rule that refused
// it.
//
// That is not an optimisation:
//
//   - `0030` keeps a scope rule append-only *because three surfaces cite its
//     id*, and one of them is this refusal. A refusal discovered mid-run would
//     exist only for the steps a run happened to reach.
//   - The client's spawn PREVIEW is this same walk without the write. Two
//     implementations of one question drift, and the one that drifts is the
//     preview — which is what a person reads before authorising a scan against
//     a client.
//
// # Six states, and the absences are the evidence
//
//	ok        it ran and exited a code the tool calls success
//	failed    it ran and broke                  exit present
//	refused   a rule said no                    exit ABSENT, rule present
//	skipped   nobody ran it                     exit ABSENT, reason present
//	running   in flight
//	pending   planned, waiting
//
// Four of the six finish with no artifact and only one of those four is a fault.
// `exit_code` is NULL for three of them and the column means **no process ever
// existed**, not "unknown" — `CLAUDE.md`'s `skipped vs failed` pair, held by a
// constraint rather than by a convention.
//
// The same rule reaches the bytes: **`bytes = 0` is an empty artifact and no
// artifact row is nothing written.**
//
// # Split the template, then substitute
//
// `tool.argv` is text and [pkg/execx.Spawn] takes a slice. The order is the
// whole safety property, because a target's name is attacker-influenced by
// definition:
//
//	fields, then substitute   ["subfinder" "-d" "x; rm -rf /"]    one element
//	substitute, then fields   ["subfinder" "-d" "x;" "rm" ...]    four
//
// A value is ONE argv element whatever is in it, and N values are N elements —
// there is no escaping to get wrong, which is the same property `pkg/execx` has
// for taking a slice at all.
//
// # A step is one invocation over many candidates — 0039
//
// `httpx -l hosts.txt` is ONE PROCESS over forty hosts, so a step is one
// invocation however many things it touches. What it touched is a
// [domain.Candidate] each:
//
//	invocation   one per step, planned up front            0033 §1 intact
//	candidate    run · invocation · kind · value ·
//	             permitted · refusal_rule · refusal_reason
//
// **The candidate is where the SCOPE PROOF lives.** `0037` put the subject on
// the invocation, which was right while a step touched one thing; an array
// replacing it was refused because the REFUSED candidates would have nowhere to
// live, and *"we would have looked at these three and a rule said no"* is
// `0010`'s whole purpose.
//
// The gate is asked PER CANDIDATE and the permitted subset is what the argv
// carries. A source step has exactly one — the target — so today's behaviour is
// the one-candidate case of the general shape.
//
// # A downstream step resolves when it RUNS, not when it is planned
//
// Its candidates are the distinct subjects its feeders observed, filtered to the
// kind its tool consumes; several feeders union. So its argv cannot exist at
// plan time, and it is written as the SPLIT, UNSUBSTITUTED TEMPLATE and
// overwritten when it runs:
//
//	pending / skipped   ["httpx" "-u" "{{host}}"]      what WOULD have run
//	ok / failed         ["httpx" "-u" "a.acme.test"]   what RAN
//
// A step whose feeders produced nothing is still `skipped`, and the reason has
// stopped being a lie: until `observation` landed it said *"nothing upstream
// produced observations to feed it"* because nothing ever could.
//
// **DERIVATION STILL CANNOT BE BUILT, and the reason changed.** One process
// given thirty-seven hosts emits two hundred URLs, and nothing in the record
// connects a URL to the host it came from. The connection is in the bytes —
// `httpx` emits `.input` — so it needs a mapping naming the field that carries a
// record's provenance, which is a decision about mappings and not about runs.
//
// # These are the engagement's rows — 0031
//
// `workspace_id`, non-null, on all three tables. A run is a claim about a
// client, which is the side of `0031`'s test that keeps the narrow key.
package run
