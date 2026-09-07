// Package query reads runs, their invocations and their artifacts.
//
// Every read takes the ENGAGEMENT and checks it. A run is a claim about a
// client — decisions/0031 — so `read` on that workspace is the gate, applied at
// the composition root.
package query
