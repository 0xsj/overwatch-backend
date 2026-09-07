// Package query answers questions about identity without changing anything.
//
// [Sessions.Authenticate] is the first of them and the reason the split exists:
// it runs on every authenticated request and needs **none** of the write side's
// ports — no transaction, no publisher, no hasher. Sharing a port set with
// `command` would drag a password hasher into the hottest path in the system.
//
// It answers with a [Caller], which is a view rather than an aggregate: what a
// middleware needs to name the actor, and nothing else.
//
// **Unknown, revoked and expired all answer [ErrNoSession].** Which of the three
// it was is not the caller's business, and distinguishing them turns the
// endpoint into an oracle for guessing tokens.
//
// **An account archived after its session was minted stops working
// immediately.** The session's own expiry cannot see that, so the account is
// read and checked on every request rather than trusted from the token.
//
// # What belongs here when the rest arrives
//
//	"my sessions"           a list, with the caller's own marked
//	api keys for an account revoked ones included — a revoked key is part of
//	                        the answer to "what happened to my key"
//
// # The rules a query follows
//
//   - **It returns a view, never a domain aggregate.** A view is shaped by the
//     screen that asked for it and may join across tables a command would never
//     touch together.
//   - **It takes no [Transactor] and no publisher.** A query that emits an event
//     has written something, and belongs in command/.
//   - **It declares its own read ports**, which do not overlap with the write
//     ports next door. The overlap people expect — "surely both need
//     AccountByID" — is where the two sides start constraining each other.
//   - **It takes any instant it needs as a parameter.** "Live" needs a time, and
//     a reader holding its own clock answers a different question from the write
//     side sharing its rows.
package query
