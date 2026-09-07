// Package target owns the thing being looked at. Everything else in the product
// hangs off it.
//
// **The reasoning about why it is a ROW lives in decisions/0029.** The short
// version is the test that record generalises: if everything a noun holds is
// derivable from rows that already exist, it is a role; if it holds
// configuration, a schedule or a lifecycle, it is a row. `asset` fails the first
// clause and stayed a word — `0009`. A target passes the second three times.
//
// # Owns
//
// The target, its name, and what kind of thing is at its root.
//
//	organisation   ASM attribution — "is this asset ours"
//	person         the entity mapper — the same machinery at a different root
//
// Those are `CLAUDE.md`'s two framings, and they are a `kind` on one table rather
// than two tables because the machinery does not differ. A third — `program`,
// for bug bounty — is named there as *later, not dropped*, and is not added
// ahead of a caller.
//
// # Does not own
//
// **Scope rules.** They hang off a target and live in `internal/scope`, which is
// a peer this package may not import — `0010` says one scope domain, and a rule
// names its target by an opaque id.
//
// **The root entity.** `0009` speaks of *"the target's root entity"* and
// `entity` does not exist. The column arrives in the migration that creates the
// row it points at; a nullable one now would be the modelled state with no
// caller `CLAUDE.md` §5 refuses.
//
// **Authorisation.** A caller's reach is org's, resolved before anything here is
// called. This package answers for any workspace id it is handed and says so.
//
// # Every row carries workspace_id, non-null, and this is the first
//
// `0005` made that a discipline nothing could enforce, because there was no
// product repository to put it in. There is now: a query here cannot be
// constructed without one, which is the move
// [[make-it-structural-not-annotated]] describes.
//
// **A product query that forgets it returns another engagement's targets** —
// with the caller authenticated, a member of the org, and nothing about the code
// path looking wrong. That is the failure this package exists to make
// impossible rather than to remember.
//
// # Archived, never deleted, and the name is released
//
// Same rule as an account, a member and a workspace, for the same reason: the
// record is what this product exists to keep. The unique index is partial over
// live rows, so a closed target's name can be reused and two live targets in one
// engagement cannot share one — a mistake nobody can see on a list.
//
// # Who may
//
//	create · rename   write    the ordinary work of adding something to look at
//	archive           admin    it hides a record, which is nearer "edit scope"
//
// Both are read off `0019`'s ladder rather than invented here.
package target
