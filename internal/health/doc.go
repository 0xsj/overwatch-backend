// Package health answers *"is the machinery well"* — `CLAUDE.md`'s one-clause
// justification for the noun: **a tool off PATH looks like silence.**
//
// # Every failure this package names looks like a clean estate
//
//	tool_unavailable    off PATH. The run FINISHED, the record is complete,
//	                    and nothing was looked at
//	tool_no_signature   a finding tool with no signature mapping — 0041 §2
//	event_buried        an outbox row that gave up. pkg/outbox says a buried
//	                    row is the alarm, and nothing read them — owed item B
//	check_unrunnable    enabled, on a clock, chainless. Skipped every tick
//	tool_unread         it spawns, its bytes are stored, nothing reads them
//	field_unmapped      the tool is saying something nobody taught us to read
//
// None of these is an error anybody sees. Each produces an empty result that is
// byte-identical to the honest empty result, which is why they need a screen
// rather than a log line.
//
// # It is DERIVED: no store, no command, no table
//
// Six reads over other domains' rows, adapted at the composition root. There is
// nothing to keep in step and nothing that can go stale, and a `health` table
// would be a second authority over facts the source tables already hold.
//
// This package imports no peer. `Probes` is expressed entirely in `health`'s own
// types and the root adapts each one, which is also where the vocabulary
// translation lives.
//
// # A clean report must enumerate what it looked at
//
// **A clean bill of health that does not say what it examined is
// indistinguishable from a report nobody ran.** So every probe carries `Looked`
// beside `Found`, and every kind gets a probe whether or not it found anything —
// a kind missing from the list is a bug in the assembler, not a clean result.
//
// This is `CLAUDE.md`'s own rule about counts, applied to the screen whose whole
// job is telling silence apart from health: *"an unmeasured total renders as `–`
// and never as `0`, because a zero nothing computed is not a zero."*
//
// # A probe that fails does not fail the report
//
// One read erroring must not blank the other five — the machinery being partly
// unobservable is exactly when somebody is looking. So a failed probe is
// recorded as `Measured: false` with a reason, the rest still answer, and
// `Report.Trustworthy` goes false.
//
// That flag is the difference between two sentences that share a symptom list:
//
//	trustworthy    "nothing is wrong"
//	not            "we found nothing IN THE PLACES WE COULD LOOK"
//
// It is a banner, not a footnote, and a client that renders the empty list the
// same way in both cases has undone the point of the package.
//
// # A symptom is counted, and dated from the OLDEST occurrence
//
// One tool off PATH across forty invocations is ONE symptom with a count — the
// same argument `0041` makes for a finding. And `Since` is the oldest, not the
// newest, because *"this has been broken since Tuesday"* is the sentence
// somebody needs; *"last seen a minute ago"* is true of everything still broken.
//
// The report is a WORKSPACE read and it works on a closed engagement, because
// asking whether the machinery was well while the work happened is a question
// that outlives the work — 0027.
package health
