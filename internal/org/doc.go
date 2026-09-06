// Package org owns who somebody is to an organisation, and what they may do.
//
// # PLACEHOLDER — not built
//
// # Owns
//
// The org, its members, their roles, and invitations. **A personal org is
// created with every account** — decisions/0005 — so there is one ownership path
// and no polymorphic owner, and inviting a collaborator is adding a member
// rather than migrating a workspace between owner kinds.
//
// **A removed member is archived, not deleted.** A claimant is an account and
// every human attribution points at that row.
//
// **Grants narrow, never widen.** A member has a role in an org; a grant may
// restrict them to particular workspaces or targets. Authorisation is
// role ∩ grants and never a union — a union is how a revoked role leaves a
// forgotten grant standing.
//
// # Does not own
//
// The account (identity's). The workspace: an org contains workspaces, and a
// container's lifecycle is not a membership question — see internal/workspace
// for why the split, and decisions/0005 for the three levels.
//
// # Emits
//
//	org.created · member.added · member.removed · role.changed
//	org.invitation.sent · invitation.accepted
//
// # Deliberately absent
//
// The role set beyond owner. The mock draws four roles and an eleven-row
// capability matrix and its own evidence map marks all of it INVENTED; a second
// invented set is no better. One role exists because a personal org needs one,
// and the rest arrives with the first person who is not the owner.
package org
