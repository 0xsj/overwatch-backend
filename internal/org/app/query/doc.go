// Package query answers questions about org without changing anything.
//
// **It is empty, and that is the current state rather than an omission.**
// Provisioning is the only operation and it is a command.
//
// What belongs here when it arrives: the member list, an account's orgs, and
// the members-by-workspace grid ALIGNMENT.md asks the client for. Each returns
// a view rather than a domain aggregate, declares its own read ports, and takes
// neither a transaction nor a publisher — see internal/identity/app/query for
// the rules in full.
package query
