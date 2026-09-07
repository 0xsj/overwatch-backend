// Package app is what target does, split by whether an operation writes.
//
// **The reasoning about the domain lives in internal/target.**
//
// The split is the same as identity's and org's: a command takes a publisher and
// a minter, a query takes neither, and neither authorises. A caller's reach is
// resolved by org before anything here runs — see root.
package app
