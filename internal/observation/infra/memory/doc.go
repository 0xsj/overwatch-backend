// Package memory is observation's repository without a database — a first-class
// adapter, not a test double.
//
// **One thing the schema holds is restated here:** `unique (artifact_id, path)`
// on the unmapped table, as an upsert that REPLACES the count rather than adding
// to it. Re-extracting the same bytes must not double the LEFT ALONE number,
// which is the denominator of the only measurement of the correction loop.
package memory
