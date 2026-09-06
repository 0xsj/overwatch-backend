// Package org owns who somebody is to an organisation.
//
// # Owns
//
// The org and its members. **A personal org is created with every account** —
// decisions/0005, provisioned in registration's single transaction alongside the
// workspace — so there is one ownership path and no polymorphic owner, and
// inviting a collaborator is *adding a member* rather than migrating a workspace
// between owner kinds.
//
// **A removed member is archived, not deleted.** A claimant is an account and
// every human attribution points at that row; the membership that authorised the
// claim has to survive it.
//
// # One role exists, and that is not an oversight
//
// `owner`. decisions/0005 records that the mock draws four roles and an
// eleven-row capability matrix and marks **all of it INVENTED** in its own
// evidence map, and that a second invented set would be no better.
// decisions/0012 names the moment the second role arrives: **the first
// invitation.** A collaborator cannot be an owner — `role ∩ grants` cannot
// narrow somebody who already implies everything in the org — so whoever writes
// the invite flow defines `member`, and defines it against a real person rather
// than a mock.
//
// # Does not own
//
// The account, which is identity's. The workspace: an org contains workspaces,
// and a container's lifecycle is not a membership question — see
// internal/workspace for why the split.
//
// **Registration.** decisions/0012 puts it above every domain: one transaction
// writes an account, a personal org and a workspace, and this package does not
// know identity or workspace exists. pkg/postgres carries the transaction on the
// context, so this repository composes into a transaction it has never heard of.
//
// # An org_id and an account_id are not foreign keys
//
// They name rows in other schemas and are deliberately unreferenced. Schema per
// domain with **no foreign key crossing out of it** is what makes extracting a
// domain into its own service a dump of one schema rather than an untangling —
// and it is the cost side of that: **referential integrity across the seam is
// the registration transaction's job, not the database's.** A member row naming
// an account that does not exist is a bug this schema cannot catch.
//
// # Deliberately absent
//
// **Grants.** decisions/0005 says a grant narrows an org role and never widens
// it. With one role, a grant has nothing to narrow: intersecting `owner` with a
// workspace grant either means nothing or means the grant is secretly widening.
// Modelling it now would fix its shape before the role set it narrows exists,
// which is exactly the state CLAUDE.md §5 warns about. **Trigger: the second
// role**, which is the first invitation.
//
// **Invitations.** They carry a role, and there is one. Same trigger.
//
// **Billing.** decisions/0005 puts it at this level and nothing charges yet.
//
// # Emits
//
//	org.created · org.member.added · org.member.archived
//
// `member.added` is a decision when a person invites somebody — decisions/0014 —
// and work when registration provisions the founding member. The emitter picks
// the constructor; this package does not decide.
//
// # Storage
//
// Schema `org`, its own migration sequence, its ledger in that schema.
// **Version is optimistic concurrency and starts at 1**, on every mutable row,
// so the rule is uniform rather than remembered.
package org
