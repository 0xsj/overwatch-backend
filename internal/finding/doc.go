// Package finding holds a vulnerability claim with a lifecycle — decisions/0041.
//
// # A finding is one problem on one fragment
//
//	finding   workspace · tool · signature · fragment    UNIQUE
//	          first_seen · last_seen · sightings
//	          state · severity · severity_by             0004
//
// `nuclei` matching one template on one URL every night for a month is ONE
// FINDING SEEN THIRTY TIMES. The board answers *"what is wrong with this
// estate"*, and one row per sighting turns it into a scan log where
// twenty-nine rows out of thirty are noise — and where **triage does not
// stick**: a ruling made on Monday would face a fresh untriaged row on Tuesday,
// forever. A ruling is about the problem, not about the run that noticed it.
//
// The TOOL is in the identity because two scanners' identifier spaces can
// collide, and nothing here can know that one tool's `weak-cipher` means
// another's. Merging them would assert that two signatures mean the same thing,
// which is the similarity claim `CLAUDE.md` bans outright.
//
// # A rescan changes nothing a person decided
//
// `Seen` moves `last_seen` and increments `sightings`. It does not touch the
// state and it does not re-assert the severity — the tool says `high` again
// tonight, and overwriting a human's override with the template's opinion every
// night is the same failure one field over.
//
// The one exception is a RESOLVED finding seen again, which reopens. There is no
// `regressed` state, so a fix that did not hold reads as a new finding; the
// evidence a reader has is an old `first_seen` beside a large `sightings`.
// `0041` names that as an accepted cost and the fifth state as what fixes it.
//
// # Four states, and two of them must never collapse
//
//	open       nobody has looked
//	triaged    somebody looked, it is real
//	resolved   THE THING WAS FIXED
//	dismissed  ruled not to matter, WITH a reason
//
// `resolved` is a change to the world and `dismissed` is a change of mind, and a
// client report cites them differently. They deliberately do not share the
// judgement quartet's words — `0009` is explicit that a finding is not a
// judgement, and reusing `unopened`/`watching` would invite exactly the collapse
// it refused.
//
// **A dismissal requires a reason and a resolution does not.** Deciding a real
// problem does not matter is a disagreement with the tool that found it, and one
// with no stated reason records THAT somebody disagreed without saying WHY THEY
// WERE RIGHT. A fix needs no argument: the thing is gone.
//
// # Severity is a claim — 0004, and this is its first caller
//
// A `nuclei` template asserting `high` is a CATEGORY, NOT A PROBABILITY, so it
// carries no confidence; storing `1.0` would destroy the distinction
// permanently. An override keeps the whole prior claim in `superseded` and must
// give a basis.
//
// # Nothing closes a finding automatically
//
// A tool that stops matching resolves nothing. Absence of a match is not
// evidence of a fix — the scan may have been refused, the target down, the
// template changed. `last_seen` going stale is the signal, and it is a READ
// rather than a write. There is deliberately no system path through `Decide`.
//
// # A detail is not an observation
//
// The name, the description, the matcher: they are about the FINDING, and an
// observation is about a SUBJECT. Filing `name: Log4j RCE` as an observation of
// the URL would put it in that asset's field list, where it reads as a property
// of the url. Same shape, different subject — and the subject is what an
// observation IS.
package finding
