// Package note holds a person's own text — decisions/0043.
//
// # It is the only input nothing else produces
//
// Every other row in this system is something a tool said, a rule decided, or a
// subscriber assembled. A note is somebody typing. That is the whole reason the
// noun exists and it is what every rule below is protecting.
//
//	note   workspace_id                    always
//	       subject_kind · subject_value    optional — the 0034 vocabulary
//	       body · author · created_at · updated_at
//
// # The subject is the TUPLE, not a foreign key
//
// `0036` made a fragment IS `(workspace, kind, value)`. A note points at the
// same tuple and holds no `fragment_id`, which buys three things:
//
//	a note SURVIVES re-observation       the fragment row is upserted; the
//	                                     tuple is stable, so the note stays
//	a note about something NEVER         "watch out for acme-staging.test,
//	observed is still a note             we have not scanned it"
//	no polymorphic foreign key           0009 rejected one, on the ground the
//	                                     database cannot enforce it
//
// **A note with NO subject is the engagement summary** — `0042`'s eighth report
// section — and it is the same row shape rather than a second table. One noun,
// two uses, and the difference is whether two columns are filled.
//
// The value is FOLDED, and it must fold the way `entity` folds: a note attaches
// by tuple, and two folds one domain apart is a note about nothing. `0037` and
// `0040` each named the fold mismatch as their own quiet failure; this is the
// third table to join on it.
//
// # It is edited in place, and that is a departure worth naming
//
// `mapping` and `scope.rule` are append-only, and `0030` says why: three
// surfaces cite a rule id, so an expression that moved under a citation makes
// that citation a lie.
//
// **Nothing cites a note that way.** The one place a note's past text is
// load-bearing is a report, and `0042` already freezes the bytes — the January
// document holds January's words whatever the note says in March. Versioning
// would buy history nobody reads, at the cost of an editor that cannot autosave.
//
// The rule this makes explicit, for the next noun that asks: **append-only is
// for what is CITED BY ID, not for everything a person can change.**
//
// # Anybody reads, only the author writes
//
// A note is useful because the engagement can see it, and trustworthy because
// nobody else can change the words under somebody's name. `Edit` and `Erase`
// both refuse a non-author; the wire carries `mine` so an editor can be hidden
// rather than offered and then refused.
//
// A closed engagement's notes are readable and not writable, and this package
// contains no rule for that — `0027` already makes a closed engagement a record
// you can read and not act in, and the note routes go through the same gate as
// everything else.
//
// # Search is NOT built, and the noun's own definition says it should be
//
// `CLAUDE.md` describes a note as *"searched beside assets and observations"*.
// There is no search surface in this system — not for assets, not for
// observations, not for anything — so a note cannot yet be searched beside
// things that cannot be searched.
//
// What is built is a LIST, filtered by subject. A half-set filter is REFUSED
// rather than ignored: `subject_kind=host` with no value asks about every host,
// and silently answering the other question would look like it worked.
//
// Cross-domain search is its own piece of work with its own decisions — what is
// indexed, what a result looks like when three domains match, whether a lapsed
// member's notes surface. **This is a noun shipped short of its own one-line
// definition, and saying so is the point.** Owed.
package note
