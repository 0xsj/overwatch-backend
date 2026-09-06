// Package app is what identity does, split by whether an operation writes.
//
// **The reasoning about the domain lives in internal/identity.**
//
//	command/   changes something. Takes a transaction, emits events
//	query/     answers something. Takes neither
//
// # The split is about ports, not about storage
//
// A command needs a [Transactor], a [Publisher] and a [Hasher]. A query needs
// none of them, and forcing it through the write side's ports drags all three
// into paths that have no use for them — session validation runs on every
// authenticated request and must not carry a password hasher to get there.
//
// **The other half is the return type.** A query answers with a view, not with
// an aggregate: rendering a list of sessions should not hydrate a domain object
// per row, and a read that is free to write its own SQL can keep things in the
// database that Go would otherwise have to be trusted with — comparing a session
// digest in the query rather than in a loop is why every other session's digest
// never leaves the server.
//
// # Light means light
//
// **One database, the same tables, no projections and no read models.** There is
// no second store, nothing is eventually consistent with anything, and a query
// reads the rows a command just wrote. Somebody who knows CQRS will arrive
// expecting the rest of it; there is deliberately no rest of it, and adding a
// projection is a decision with its own record rather than a completion of this
// one.
package app
