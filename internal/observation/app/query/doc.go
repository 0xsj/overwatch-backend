// Package query reads observations, the paths nobody mapped, and LINEAGE.
//
// Lineage is a PROJECTION and it lives here because "a projection lives with the
// root of its traversal and declares ports for the rest" — the root is the
// observation. It walks into `tool`, `run` and `scope`, none of which this
// package may import: they are ports, adapted at the composition root.
package query
