// Package query answers questions about workspace without changing anything.
//
// [Workspaces.InOrg] is the switcher's list: the live workspaces of one org,
// oldest first. Archived ones are excluded in the SQL rather than by the caller,
// because a screen that must not show them cannot be relied on to remember, and
// anybody wanting them back asks a different question.
//
// # It does not authorise, and that is deliberate
//
// [Workspaces.InOrg] takes an org id and answers. It never asks whether the
// caller belongs to that org, because this package cannot see a membership —
// org owns that table and decisions/0017 forbids the join. A query that
// silently authorises is the shape of an access bug: it looks safe at the call
// site and is wrong at every other one. The caller resolves the org first, which
// is what root's /v1/me does before it asks anything here.
//
// What belongs here when it arrives: whatever the breadcrumb needs to name one
// workspace, and the per-workspace counts the overview shows. Both are views
// rather than domain aggregates — see internal/identity/app/query for the rules.
package query
