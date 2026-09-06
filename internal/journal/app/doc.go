// Package app is the journal's one behaviour: record every event that reaches
// it.
//
// **The reasoning lives in internal/journal.**
//
// [Subscriber.Handle] is an [events.Handler] and has **no filter**, which is the
// difference from audit's. Every unit of work is a line, decision or not, and
// the `decision` column is what lets a reader join the two tables rather than
// choose between them.
//
// [Log] is declared here and satisfied by internal/journal/infra/postgres.
package app
