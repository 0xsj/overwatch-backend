// Package scope decides what a tool may touch, and says WHY the answer is no.
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
// Include and exclude rules. Exclude beats include.
//
// TWO GATES — decisions/0010 — because one word was doing two jobs:
//
//	spawn	may a process touch this?      host · cidr · ip
//		evaluated before exec, fails as a REFUSAL with no artifact.
//		Carries a tool-kind qualifier: passive | light | loud.
//
//	claim	is this inside the engagement?  repo · email · account · document
//		evaluated at attribution, fails as NO SCOPE PROOF. Never refuses a
//		process, because nothing spawns at a repository. Carries no tool list.
//
// A refusal records the winning rule AND every losing rule that matched. A
// verdict alone cannot answer a client asking why, and replaying the rule set
// later is not the same question — it may since have been edited.
//
// A wildcard does not match the apex: *.example.com does not match example.com.
// The other reading silently widens scope, which is the worse failure here.
//
// # Does not own
//
// Enforcement. The runner asks before it spawns, so a refusal is an invocation
// rather than a display filter.
//
// # Emits
//
// \tscope.rule.added · rule.removed · rule.changed
package scope
