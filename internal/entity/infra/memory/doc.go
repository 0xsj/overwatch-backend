// Package memory is entity's repository without a database — a first-class
// adapter, not a test double.
//
// **Three things the schema holds are restated here:**
//
//	fragment_identity            the dedup, folded — a fragment IS the tuple
//	entity_one_root_per_target   one root, and a redelivery makes no second
//	unique (entity_id, fragment) one claim per pair
//
// And one thing the schema CANNOT hold: [Store.Assets] re-states the `asset`
// VIEW's predicate. A view is the one place a memory adapter has to duplicate a
// query rather than a constraint, and it is exactly where the two will drift —
// so the predicate is written here in the same order the SQL states it.
package memory
