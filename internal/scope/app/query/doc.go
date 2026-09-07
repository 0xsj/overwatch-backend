// Package query reads a target's scope, and answers the question it exists for.
//
// [Rules.Decide] returns a [domain.Decision] rather than rows, so no caller
// reimplements `exclude beats include` — and so the answer carries every rule
// that matched, which decisions/0010 requires and a caller filtering rows itself
// would have to reconstruct.
//
// **It does not authorise.** Editing scope needs `admin` on the engagement
// (0019); reading it needs `read`. Both are resolved by the composition root.
package query
