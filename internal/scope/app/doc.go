// Package app is what scope does, split by whether an operation writes.
//
// **The reasoning about the domain lives in internal/scope.**
//
// There is no `Edit`. A rule is added or superseded — decisions/0030 — so the
// command side has two verbs and the query side has three reads, one of which
// answers a question rather than returning rows.
package app
