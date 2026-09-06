// Package query answers questions about identity without changing anything.
//
// **It is empty, and that is the current state rather than an omission.**
// Nothing reads identity yet: registration is the only operation, and it is a
// command. The package exists so the convention is visible before there are
// twenty files to move.
//
// # What belongs here when it arrives
//
//	session validation      on every authenticated request. The hottest path
//	                        in the system, and the reason the split exists
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
