// Package workspace owns the container work happens in.
//
// # PLACEHOLDER — not built
//
// # Owns
//
// The workspace and its lifecycle. CTF1, CTF2, a client engagement — a solo
// hunter has several at once and they must not see each other's targets, scope
// rules or findings. Every workspace belongs to an org; there is no other kind
// of owner — decisions/0005.
//
// **Every row in every product domain carries workspace_id, non-null, from its
// first migration.** provenance.Tenant means this, not the org: the org is
// derivable from the workspace, and the workspace is what scopes a query.
//
// # Why this is not part of org
//
// decisions/0005 assigns three LEVELS and does not assign packages, so this is
// a shape decision rather than a reversal.
//
// Every product domain needs to know a workspace exists. If that lived in org,
// the dependency would drag billing, membership and invitations along the
// product path — and the import checks now enforce that a unit may not import a
// sibling, so the cost would be paid as a port anyway. The lifecycles differ
// too: an org is created once at signup, workspaces are made and archived
// constantly.
//
// # Does not own
//
// Membership and roles, which are org's — access to a workspace is a grant that
// narrows an org role. Targets: a target belongs to a workspace, and target
// owns the row.
//
// # Emits
//
//	workspace.created · workspace.archived
package workspace
