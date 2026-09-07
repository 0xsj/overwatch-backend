// Package scope answers two questions that look like one.
//
// **The reasoning is in decisions/0010 and 0030.** `0010` fixed the rule's
// shape and both gates; `0030` closed the question it left open by name.
//
//	spawn   may a tool touch this?        host · cidr · ip
//	        evaluated BEFORE exec, and failure is a REFUSAL with no artifact
//
//	claim   is this inside the engagement? repo · email · account ·
//	        document · org · person
//	        evaluated AT ATTRIBUTION, and failure is not a refusal — it is the
//	        absence of a finding's scope proof
//
// **A claim rule never refuses a process**, because nothing spawns against a
// repository. It answers a different question and produces a different record —
// which is why the two live in one table with a discriminator rather than in two
// nouns: both need rule ids, both need `exclude beats include`, and both need
// the same audit actions.
//
// # Three rules that decide everything
//
// **Exclude beats include**, on both gates. A refusal names the winning rule AND
// every losing rule that matched — a verdict alone cannot answer a client asking
// why, and reconstructing it later means replaying a rule set that may have
// changed since.
//
// **Nothing is in scope until a rule says so.** A target with no rules permits
// nothing. A default of "permit unless excluded" turns a forgotten rule set into
// an authorisation to touch anything, and that failure is silent; the opposite —
// a refusal on a target nobody scoped — is loud and fixed by writing the rule you
// meant to write.
//
// **`*.example.com` does not match the apex.** It matches `a.example.com` and
// `a.b.example.com`. Sealed in `0010` because wildcard semantics are agony to
// change once rules exist in the field, and because the safer reading never
// silently widens.
//
// # Append-only, because three surfaces cite a rule id
//
// A rule is added or superseded, never edited — `0030`. `r3` appears on an asset
// row, on an invocation refusal and on a finding's scope proof, and each captures
// it AT THE TIME. A row whose pattern changed under a citation makes every one of
// those citations a lie, silently and retroactively.
//
// # Matching is on the ADDRESS, never on the text
//
// `10.0.0.1` and `10.000.000.001` are the same address and different strings;
// an IPv4-mapped IPv6 address is the same host wearing a different spelling. A
// `cidr` rule matches an `ip` candidate — `10.0.0.0/8` is a statement about every
// address in it — and an `ip` rule never matches a `cidr`, because
// "10.1.2.3 is in scope" says nothing about the range containing it.
//
// # What this package does not do
//
// **It does not authorise.** Editing scope needs `admin` on the engagement —
// `0019` — resolved by the composition root before anything here runs.
//
// **Nothing evaluates `spawn` yet**, because `invocation` does not exist. The
// evaluator is written and called only by its own tests, which is the modelled
// state `CLAUDE.md` §5 warns about — accepted deliberately, because the
// alternative is a rule table with no function giving it meaning and a shape
// discovered to be wrong when the runner arrives.
package scope
