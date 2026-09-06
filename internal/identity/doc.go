// Package identity owns accounts, credentials and sessions.
//
// # PLACEHOLDER — not built
//
// A contract before there is code, so the document leads the implementation.
// custody 0010-0014 measured why that ordering matters: where doc.go states the
// behaviour a barriered suite dominates, and where the code moved past the
// document the suite can see nothing.
//
// # Owns
//
// The account, its credentials, and its sessions. An account is the identity a
// human attribution, a judgement and an audit entry all point at.
//
// **An account is never deleted, only archived** — decisions/0005. Every one of
// those rows names it, and they are the record this product exists to keep.
// Deleting it orphans them or forces a cascade that destroys evidence.
//
// # Does not own
//
// Roles and membership, which are org's. Who may see what, which is a policy
// the composition root applies. The workspace, which is a container rather than
// a permission.
//
// # Emits
//
//	identity.account.created · credential.changed
//	identity.session.started · session.ended · account.archived
//
// **The first subscribers are already implied and are not hypothetical.** A
// personal org is provisioned on signup — decisions/0005 — and sessions are
// invalidated when a credential changes. Two consumers, neither of them audit,
// which is the argument decisions/0007 makes for an outbox over a direct port.
//
// # Deliberately absent
//
// Authorisation. This answers who somebody is; what they may do is org's, and
// enforcing it is the transport's.
package identity
