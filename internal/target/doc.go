// Package target is the thing being looked at, and everything else hangs off it.
//
// # PLACEHOLDER — not built
//
// This file exists so the directory has a name and a contract before it has
// code. Written 2026-09-06 from drafts/domain-map.md, which binds nothing. It is
// expanded into a real contract by the session that builds this domain, and that
// expansion happens BEFORE the implementation — custody 0010-0013 measured why:
// where the document states the behaviour a barriered suite dominates, and where
// the code moved past the document it can see nothing.
//
// # Owns
//
// The target, and the workspace it lives in — decisions/0005.
//
// # Does not own
//
// Scope rules, though every rule names a target. Nothing about what has been
// found; a target is a subject, not a record.
//
// # Emits
//
// \ttarget.created · target.archived
package target
