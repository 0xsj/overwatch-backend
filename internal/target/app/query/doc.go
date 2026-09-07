// Package query answers questions about targets without changing anything.
//
// **It does not authorise**, and every method takes a workspace id it answers
// for without checking. The caller resolves its reach first — a query that
// silently authorised would look safe at one call site and be wrong at every
// other one.
//
// Two lists rather than one with a flag: [Targets.Live] feeds the screen, and
// [Targets.All] includes archived ones, which is what makes a closed target
// reachable and therefore reopenable. The same pair `workspace` uses.
package query
