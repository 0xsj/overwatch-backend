// Package http is identity's edge. It maps a request onto a command and an
// error onto a status code, and it holds no rules of its own.
//
// **A handler that decides anything is a rule in the wrong place.** The password
// policy, the email shape and the transaction all live in internal/identity/app;
// what is here is decoding, one call, and encoding.
//
// # The registration response says nothing a caller has not earned
//
// It returns the account id, the email as stored, and the ids of the org and
// workspace that were provisioned — because the client needs the workspace id to
// build its next URL, which is what ALIGNMENT.md asks for.
//
// It does **not** return the account's status, its name, or anything about the
// credential. A response is a commitment: every field in it is one somebody will
// build against, and the ones that are not needed yet are cheaper to add than to
// withdraw.
//
// # Errors go out as kinds, never as sentences
//
// httpx.WriteError maps errors.Kind onto a status and hides the cause of an
// Internal. So a duplicate address arrives as Conflict and a short password as
// Invalid, without this package matching on a string or knowing a domain
// sentinel by name.
//
// **A registration failure must not become a user directory.** Conflict on a
// duplicate email tells an unauthenticated caller that an address is registered,
// which is a real disclosure and is the standard trade — the alternative is to
// accept every registration and send a "you already have an account" email, which
// needs mail this build does not have. Recorded rather than decided by accident;
// the trigger to revisit is the verification flow.
package http
