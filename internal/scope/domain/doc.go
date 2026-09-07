// Package domain holds a scope rule and the evaluator over a set of them.
//
// **The reasoning lives one level up, in internal/scope.**
//
// [Decide] is a pure function of a rule set and a candidate. It reads no clock,
// touches no database and returns every rule that matched — so a refusal can
// explain itself without replaying a rule set that may have changed since.
package domain
