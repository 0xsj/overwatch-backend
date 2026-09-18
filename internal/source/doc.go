// Package source owns user-supplied research material and immutable captures.
// A reference records a URL without fetching it. Paste and import preserve the
// submitted UTF-8 bytes; neither path executes a collector or tool.
//
// Capture versions identify what was retained at a particular time. Appending
// a version does not move an existing citation. Source metadata and capture
// metadata live in the source schema; content-addressed bytes use pkg/blob.
// Metadata and audit publication share a transaction. Blob writes precede the
// transaction, so failed writes may leave unreferenced bytes but cannot expose
// a source or capture without its audit event.
//
// Statements citing these bytes belong to observation. The composition root
// adapts a retained capture into observation's validation port, preserving the
// distinction between material, a source statement, and working interpretation.
package source
