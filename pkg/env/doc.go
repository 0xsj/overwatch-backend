// Package env turns a key-value lookup into typed values, collecting every
// problem rather than stopping at the first.
//
// It is deliberately generic. What settings a binary needs is a fact about that
// binary, so the Config struct lives at its composition root; what this package
// owns is the machinery, which is identical for every service that will ever be
// split out of this one.
//
// # Read once, at the edge
//
// A [Reader] is constructed in the composition root, every value is read from
// it, [Reader.Err] is checked once, and the reader is discarded. There is no
// package-level state and no env.Get(): a value read deep in the tree is a
// dependency nothing declared, and it makes the consumer impossible to
// construct differently in a test.
//
// If the process started, every value it needs was present. That property is
// lost the moment one value is read lazily, because the failure then arrives
// days later on the one path nobody exercised.
//
// # Lookup is a port
//
// [Lookup] is a function, so sources compose. [OS] reads the process
// environment and [Map] reads a map for tests. A future resolver that turns
// "file:///run/secrets/db" into the file's contents is another Lookup wrapping
// this one, and nothing in this package changes to allow it.
//
// # Absent, empty and set are three states
//
// A [Lookup] reports presence separately from value, so "the operator said
// nothing" and "the operator said nothing-in-particular" stay distinguishable.
// Collapsing them is how an empty string becomes a silently accepted value.
//
// Each getter declares what the three mean, and the table is the contract:
//
//	                       absent          empty ("")       set
//	Required               problem         problem          the value
//	String(fallback)       fallback        "" is the value  the value
//	RequiredInt            problem         problem          parsed, or problem
//	Int(fallback)          fallback        problem          parsed, or problem
//	Enum(fallback, ...)    fallback        problem          must be allowed
//	Secret                 problem         problem          the value
//
// [Reader.Secret] has no fallback form on purpose. A credential with a default
// is a credential somebody will ship, and the default is what runs in
// production when the real one is missing.
//
// # Every problem at once
//
// A getter that fails records the problem and returns the zero value; it does
// not panic and does not stop the reader. That rule is about OPERATOR input.
// Caller error is a different category and is not softened: [Reader.Enum] panics
// when its own fallback is not in the allowed set. No environment can cause that
// and no operator can fix it, recording it would blame them for a programming
// mistake, and returning it would let the fallback escape the closed set the
// getter exists to enforce. [Reader.Err] then reports all of them
// in a single error of kind errors.Invalid, carrying errors.Fields keyed by the
// variable name:
//
//	{"DATABASE_URL": "required", "PORT_SERVER": "not a number: seven"}
//
// One problem per run turns a fresh checkout into a sequence of restarts, and
// the operator never learns how much is left. [Reader.Err] returns nil when
// nothing failed, and the values read before a failure are still whatever the
// getters returned — a Reader with a non-nil Err has produced no usable
// configuration and its results must not be used.
//
// # Declared is the boot manifest
//
// [Reader.Declared] returns every variable that RESOLVED, sorted
// lexicographically by [Var.Key]: what it resolved to, what the fallback was,
// whether it came from the environment or from that fallback, and whether it is
// a secret. Logging it once at startup answers "what is this process actually
// running on", which is the question a container running on an unintended
// default cannot otherwise answer.
//
// Lexicographic rather than call order, because a manifest is read to find a
// key, and call order changes whenever somebody reorders a struct literal — a
// diff in the boot log that means nothing happened.
//
// A key that produced a problem is NOT in the manifest; it is in [Reader.Err],
// and the two are disjoint. That follows from the rule above — a Reader with a
// non-nil Err has produced no usable configuration, so its manifest describes
// nothing and must not be logged as one. Check Err first. Declared is
// meaningful only when Err is nil, and in that case there are no failures to
// have omitted.
//
// The invariant this buys: [Var.Set] false means exactly one thing, that the
// value came from the fallback. It cannot also mean "this failed", so a reader
// of the manifest never has to cross-reference Err to know which.
//
// Each resolved read appends one [Var], so reading the same key twice yields two
// entries and the duplicate is visible rather than silently collapsed. Problems
// are keyed by variable name instead, so two problems on one key leave the later.
//
// Declared returns a COPY, and sorts the copy rather than the reader's own
// storage. A caller handed a reference into a Reader's internals can reorder or
// overwrite the manifest from the outside, and that is a bug written once and
// found a year later. errors.FieldsOf copies for the same reason; a package that
// hands out its internals in one place and not another teaches nobody which is
// which.
//
// Added 2026-09-05, after a mutation removing the copy survived the whole suite.
// The rule was true all along and this document had never said it, so nothing
// tested it.
//
// [Var.Default] is kept beside [Var.Value] rather than folded into it, so a
// manifest can render "info (default)" and distinguish it from an operator who
// set LOG_LEVEL=info deliberately. Those are the same value and different
// facts, and the second one is not a thing to change without asking.
//
// A getter taking no fallback — [Reader.Required], [Reader.RequiredInt],
// [Reader.Secret] — records Default as "". That is unambiguous rather than a
// collapsed state: such a getter reaches the manifest only by resolving from the
// environment, so [Var.Set] is necessarily true and Default is known to be
// unused.
//
// A non-string fallback is recorded in ordinary decimal form. Int(k, 8080)
// records Default "8080", and [Var.Value] for a defaulted key is that same
// string.
//
// [Var.String] renders one line — KEY=value, with " (default)" appended when the
// value came from a fallback rather than the environment. A boot log of raw
// structs is technically the same information and is not the same answer to
// "what is this process running on"; one line per variable is.
//
// An empty or whitespace-bearing value is quoted, so KEY="" is visibly empty
// rather than looking like a truncated line. That is the absent/empty/set
// distinction surviving all the way to the output, having been preserved by
// every getter on the way.
//
// [Var.Value] is already redacted for a secret, so the manifest is safe to log
// whole. That is a property of this package rather than a rule its callers must
// remember.
//
// # What is deliberately absent
//
// No struct tags and no reflection binding. Tag-driven decoding is less typing
// and it collapses absent and empty, which is the distinction this package
// exists to keep.
//
// No Duration, Bool or Ratio getters yet. They are three lines each and they
// arrive with their first caller, not before.
//
// No secret reference resolution — "file://", "env://" and the rest. It is a
// [Lookup] decorator by shape, taking one and returning one, so it composes at
// the composition root and needs nothing from here.
//
// It also must not live here. Resolving a file reference means touching the
// filesystem, and this package touches none. [OS] calls os.LookupEnv and that is
// the whole of its contact with the world — a process-local map read that cannot
// fail, cannot block, and cannot be permission-denied. Adding file access would
// change the failure modes from parsing to parsing plus IO, and would mean a
// test could no longer be driven by [Map] and nothing else.
//
// Corrected 2026-09-05: this paragraph previously claimed the package imports no
// os at all, which was false the moment [OS] existed. The property that matters
// is filesystem access, not the import. The decorator gets its own package when a deployment needs
// one, named for the job — resolution — rather than for the subject, so it does
// not sit one letter away from pkg/secret.
package env
