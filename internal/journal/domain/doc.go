// Package domain holds the journal line and the one function that makes it.
//
// **The reasoning lives one level up, in internal/journal.**
//
// It imports pkg/events and pkg/provenance and no domain. A line is very nearly
// the envelope flattened, which is the point: the work of deciding what a
// subscriber may see was done in decisions/0013, and this package is what it
// bought.
package domain
