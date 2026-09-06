// Package invocation is one execution: its argv, its exit, and its raw bytes.
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
// The invocation and its artifact — bytes kept verbatim, content-addressed,
// with the hash and size on the row. A REFUSAL is a first-class invocation with
// no artifact: the evidence a tool did not run, carrying the scope reason.
//
// # Does not own
//
// What the bytes mean. That is observation's, and the split is what keeps the
// bytes unparsed and quotable.
//
// # Emits
//
// \tinvocation.started · finished · failed · refused
package invocation
