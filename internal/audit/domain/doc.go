// Package domain holds the audit entry and the two rules that turn an event
// into one.
//
// **The reasoning lives one level up, in internal/audit.** That file is the
// contract a spec-test suite is written from.
//
// It imports pkg/events and pkg/provenance and no domain, which is the boundary
// stated structurally: this package cannot name an account, a target or a
// finding, so it cannot come to depend on their shapes.
package domain
