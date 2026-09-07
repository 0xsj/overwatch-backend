// Package http is identity's edge. It maps a request onto a command and an
// error onto a status code, and it holds no rules of its own.
//
// **A handler that decides anything is a rule in the wrong place.** The password
// policy, the email shape and the transaction all live in internal/identity/app;
// what is here is decoding, one call, and encoding.
//
// # The surface
//
//	POST   /v1/accounts                 register
//	POST   /v1/sessions                 sign in
//	DELETE /v1/sessions/current         sign out
//	POST   /v1/verifications            {email} -> 202, always
//	POST   /v1/verifications/confirm    {token}
//	POST   /v1/password-resets          {email} -> 202, always
//	POST   /v1/password-resets/confirm  {token, password}
//
// `GET /v1/me` is NOT here. It answers "who am I and where may I go", and the
// second half is org's and workspace's vocabulary — so it is composed at the
// composition root, which is the one place allowed to know two domains.
//
// **These name resources, not verbs.** `POST /v1/accounts` creates an account
// the way `POST /v1/sessions` creates a session; `/v1/register` was a verb and
// is gone. Both answer 201: a session is a thing that now exists, and a client
// that treats 200 as the success case will be wrong about one of them.
//
// # A token travels in the BODY and never in a URL
//
// A token in a path or a query string is written verbatim into every access log
// it passes, survives in browser history, and leaks onward in a Referer header
// to whatever the next page loads. None of that is under this server's control,
// and all of it outlives the token's usefulness to its owner.
//
// So `/v1/verifications/confirm` takes `{"token": "..."}` in a POST body. The
// EMAILED link still carries the token as a query parameter — it has nowhere
// else to put it — but it points at the CLIENT (`BASE_URL/verify?token=…`),
// which reads it and posts it here. The exposure is one origin the product owns
// rather than every intermediary on the way.
//
// # The two endpoints that send mail answer 202 unconditionally
//
// `POST /v1/verifications` and `POST /v1/password-resets` return 202 for an
// address that exists, one that does not, and one that is not an address at all.
// **An endpoint that says "no such account" is a user directory anybody can read
// one address at a time**, and these two are unauthenticated by necessity —
// somebody who cannot sign in is exactly who needs them.
//
// This is a different trade from registration, which DOES answer Conflict on a
// duplicate address, and the difference is that registration cannot avoid it:
// the alternative is accepting every registration and mailing "you already have
// an account", which is a flow rather than a status code. Recorded rather than
// decided by accident.
//
// **Both are rate limited by client address, with a separate budget each.**
// Without a limit they are a way to post mail from this product's domain to any
// address at whatever rate a script manages. The budgets are separate so that
// exhausting one does not close the other: somebody who cannot receive their
// verification mail must still be able to reset a password.
//
// # The registration response carries no tenancy, and that is decisions/0017
//
// It returns the account id, the email as stored, and the **status** — which is
// `pending`. At the moment it responds, neither an org nor a workspace exists:
// registration publishes a fact and the chain provisions from it, each link in
// its own transaction. A client that needs a workspace id asks `/v1/me`, which
// is also the endpoint that will say so when the chain has not finished yet.
//
// It does not return the name or anything about the credential. A response is a
// commitment: every field is one somebody will build against, and the ones not
// needed yet are cheaper to add than to withdraw.
//
// # A pending account signs in, and that is decisions/0018
//
// Sign-in succeeds for `pending`. Verification gates CAPABILITY, not the door —
// gating the door made a mistyped address a permanent lockout and let a stuck
// subscriber lock out a verified user. The response carries the status so the
// client can show the banner; the refusal happens at the workspace gate.
//
// # Errors go out as kinds, never as sentences
//
// httpx.WriteError maps errors.Kind onto a status and hides the cause of an
// Internal. So a duplicate address arrives as Conflict, a short password as
// Invalid, and a token that is unknown, spent or expired as ONE Unauthenticated
// — telling a caller which of the three would tell somebody holding a guess
// which half of the guess was wrong.
package http
