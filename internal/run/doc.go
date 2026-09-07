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
// # Downstream steps are `skipped`, and that is true today
//
// Feeding step two needs to know what step one FOUND, which is a `mapping`
// applied to an artifact producing an `observation` — and `observation` is
// UNBUILT. So a run executes its SOURCE steps and writes every downstream step
// as `skipped`, with a reason naming what did not arrive.
//
// Not faked, not deferred. `skipped` is genuinely reached, correctly.
//
// # These are the engagement's rows — 0031
//
// `workspace_id`, non-null, on all three tables. A run is a claim about a
// client, which is the side of `0031`'s test that keeps the narrow key.
package run
