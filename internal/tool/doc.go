// Package tool is a definition and a field mapping, never an integration.
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
// The tool: its kind, its argv template, and the mapping from its output to
// fields. The mapping is what makes coverage answerable — it declares which
// fields a run of this tool WOULD have produced.
//
// # Does not own
//
// Execution, and the parsing itself. It owns the mapping parsing follows.
//
// # Emits
//
// \ttool.added · tool.mapping.changed · tool.reviewed
package tool
