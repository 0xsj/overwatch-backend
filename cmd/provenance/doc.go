// Command provenance is a runnable illustration of pkg/provenance. It touches no
// database, opens no port, and exists to be read and run.
//
// **Provenance is the product's central claim in miniature.** Overwatch answers
// "what do I know and how do I know it", and every answer it gives is a record
// pointing at the record that caused it. Get this wrong and the lineage is a bag
// of rows sharing an identifier — which looks the same in a database and answers
// nothing.
//
// # What it prints
//
// Four chains, each demonstrating something the package's rules exist for:
//
//	1  a person clicks Run now      an origin, derives, and the tree shape
//	2  the nightly sweep            the SAME shape, and why origin is a
//	                                separate question from actor
//	3  a redelivery                 Retry: only attempt moves
//	4  a runaway                    depth as a bound rather than a statistic
//
// It also shows the case that is easy to state and hard to believe until it is
// on a screen: after [provenance.Provenance.Adopt] takes an upstream
// correlation, a depth-0 value is **no longer a root**. It opened this process's
// work; it did not open the chain.
//
// # Why a binary rather than a test
//
// The tests assert; this explains. Both exist — root/provenance_e2e_test.go
// asserts these same properties against the real HTTP edge and the real journal.
// This one is for the reader who has to hold the model in their head before the
// assertions mean anything, and CLAUDE.md says a teaching artifact outranks
// shipping advice.
//
//	go run ./cmd/provenance
package main
