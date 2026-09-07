// Package query answers questions about org without changing anything.
//
// [Orgs.For] is the first of them, and it exists because /v1/me has to answer
// "where can I go" for a caller who has just signed in. The answer is a
// [Membership] — an org and the role held in it — which is a JOIN of two tables
// the write side never touches together.
//
// # An account belonging to nothing is an answer, not an error
//
// [Orgs.For] returns an EMPTY SLICE for an account with no memberships. That is
// the state of every account between registering and the chain finishing
// (decisions/0017), and after decisions/0018 such an account can be signed in and
// looking at the screen. A NotFound would force the caller to tell "nowhere yet"
// apart from "the query broke", and they are not the same thing.
//
// # A membership that vanishes mid-answer drops out of the list
//
// The join put the org there and a second read fetches the role; between them
// an archive can land. The row is skipped rather than failing the request,
// because the caller asked where they may go and "not there" is correct.
//
// What belongs here when it arrives: the member list, and the members-by-
// workspace grid ALIGNMENT.md asks the client for. Each returns a view rather
// than a domain aggregate, declares its own read ports, and takes neither a
// transaction nor a publisher — see internal/identity/app/query for the rules in
// full.
package query
