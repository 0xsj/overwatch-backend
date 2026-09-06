// Package workspace owns the container work happens in.
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
// # An account is never without one
//
// decisions/0012. Registration writes an account, a personal org and a workspace
// in one transaction, so the client may assume a workspace always exists and no
// product write path needs a "no workspace yet" branch.
//
// **The provisioned workspace is an ordinary row.** No `is_default`, no
// `is_personal`, for the reason decisions/0005 rejected the polymorphic owner: a
// flag creates two code paths and keeps them forever so that a rename can be
// treated as special. A workspace is a workspace.
//
// **Zero workspaces is reachable and is not repaired.** Archiving your last one
// is a deliberate act, and silently re-provisioning would undo a decision
// somebody made. That state is an empty state with a create action — a different
// path, for a different reason.
//
// # Why this is not part of org
//
// decisions/0005 assigns three LEVELS and does not assign packages, so this is a
// shape decision rather than a reversal.
//
// Every product domain needs to know a workspace exists. If that lived in org,
// the dependency would drag billing, membership and invitations along the product
// path — and the import checks enforce that a unit may not import a sibling, so
// the cost would be paid as a port anyway. The lifecycles differ too: an org is
// created once at signup, workspaces are made and archived constantly.
//
// # Does not own
//
// Membership and roles, which are org's — access to a workspace is a grant that
// narrows an org role, and decisions/0012 records that no grant exists until a
// second role does. Targets: a target belongs to a workspace, and target owns
// the row.
//
// # org_id is not a foreign key
//
// It names a row in another schema and is deliberately unreferenced — schema per
// domain, no foreign key crossing out, which is what makes decomposition a dump
// of one schema. The cost is named rather than hidden: **a workspace naming an
// org that does not exist is a bug this schema cannot catch**, and keeping that
// true is the registration transaction's job.
//
// # Archived is terminal, and the name is released
//
// An archived workspace keeps its row forever — every target, observation and
// audit entry names it. The unique index on a name is therefore **partial**: it
// covers live rows only, or the first engagement called "Acme Q3" would burn
// that name inside the org for good.
//
// # Emits
//
//	workspace.created · workspace.archived
//
// # Storage
//
// Schema `workspace`, its own migration sequence, its ledger in that schema.
package workspace
