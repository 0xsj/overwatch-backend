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

// # Five roles, four rungs, and one min() — decisions/0019
//
// GitHub is the reference and Slack deliberately is not. Slack is
// public-by-default channels inside one workspace, so a member sees the channel
// list; GitHub is private-by-default repositories, so a member with no grant
// cannot see that one exists. A member who can list this product's workspaces
// can list a consultancy's client list, which is the thing it is under NDA
// about.
//
//	ORG ROLE — who you are in the firm. One per member, per org
//	  owner    billing, transfer and deletion. Always at least one
//	  admin    people and tools: invite, remove, change a role
//	  member   does the work. NO workspace access by default
//	  guest    external. Time-boxed, named workspaces only
//	  client   receives deliverables. NOT a rung — see below
//
//	WORKSPACE GRANT — what you may do on one engagement. ORDERED
//	  none     invisible. Absent from the list, never a disabled row
//	  read     the record, lineage, the invocation log
//	  write    + judgement, note, accept/reject, passive tools
//	  admin    + loud tools, edit scope, manage this workspace's grants
//
// **[Effective] is the whole rule and it lives in one function:**
//
//	min(role.Ceiling(), max(grants))
//
// GitHub's union is inside the max, where it is safe — two grants on one
// workspace take the better. decisions/0005's intersection is the outer min,
// and that is what stops a forgotten grant surviving a demotion.
//
// **An empty grant set is `none`.** The intersection of no grants is not
// everything. This is the half `0005` left ambiguous and it is the difference
// between a member seeing one engagement and seeing the whole firm's.
//
// **The org owner is the one exemption, and admin is not.** An owner is admin
// everywhere with no row, because the firm's principal is accountable for every
// engagement it runs. An admin manages PEOPLE: they invite, remove and change
// roles, and must be granted an engagement like anybody else. Those are two
// capabilities and a firm under per-client NDAs must be able to hand them out
// separately.
//
// **`client` is a role and not a rung, because it is off the ladder.** A client
// may generate a report — a write-shaped act — and may not see the invocation
// log, which is a read-shaped one. CLAUDE.md lists that pair among the ones that
// must never collapse. Keeping the oddity in the role leaves the ladder totally
// ordered, which is what lets the intersection be a min().
//
// # The grant table lives here, and the reason is the gate
//
// It is `(account_id, workspace_id, level)` with no foreign key leaving the
// schema — `workspace_id` is an opaque column, the same shape `member.account_id`
// already has for identity's id. Every authorisation check reads the role and
// the grant TOGETHER, and decisions/0017 forbids a join across schemas, so
// splitting the two inputs of one min() across two schemas would make the check
// two reads now and two network calls the day these become services.
//
// **Revoking deletes the row rather than storing `none`.** A stored `none` gives
// absence two spellings, and the two disagree the first time a query remembers
// only one of them. The unique index is therefore TOTAL, unlike
// `member_live_account` — a grant is a live permission, not a claim somebody
// authored, and nothing in the record points back at it.
//
// # The gate refuses with NotFound and never with Forbidden
//
// [app/query.ErrNoAccess] is NotFound. A workspace the caller has no grant on, a
// workspace that does not exist, and an org they do not belong to all answer
// identically. **A 403 tells an analyst that a client they cannot see exists**,
// which for the client behind that wall is the leak itself.
//
// This is decisions/0018's owed capability gate, made specific by `0019`: resolve
// the caller's role, resolve their grants, take the min, refuse when the result
// is `none`. A workspace operation calls it FIRST and uses nothing it returns
// until it has.
//
// # At least one live owner, and no constraint can hold it
//
// It is a property of a SET of rows under a predicate, and Postgres has no
// assertion. It lives in the command that changes a role or archives a member,
// which counts live owners first, and [domain.ErrLastOwner] is what it raises.
// Stated here because it is the kind of rule that looks like it should be in the
// migration and is not.
package org
