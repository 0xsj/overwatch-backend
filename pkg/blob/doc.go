// Package blob keeps bytes and gives back their hash.
//
// It is the store under `artifact`: a tool's stdout, a fetched page, a model's
// request and response — decisions/0015 makes those the same kind of thing.
// What the domain records is a hash and a size; what lives here are the bytes.
//
// # Content-addressed, which decides three things at once
//
// A blob's name IS the sha256 of its contents, so:
//
//   - **Identity is the hash.** Two runs that fetched the same page store one
//     copy and both cite it, which is the dedup the corpus needs without a
//     dedup table to keep consistent.
//   - **Tampering is detectable.** Re-hash and compare. A record that cites
//     bytes which no longer hash to their name is a record that stopped being
//     true, and that is the difference between an archive and a claim.
//   - **A write is idempotent.** Putting the same bytes twice is one file and
//     one answer, so a retried invocation cannot produce a second artifact.
//
// # The filesystem first, and the port is what makes that a decision
//
// STACK.md: filesystem first, S3-compatible behind the same port when there is
// a reason. `Put` and `Open` are the whole surface, and neither mentions a path
// — a caller never learns where a blob lives, which is what keeps the second
// implementation a swap rather than a migration.
//
// # A write is a temp file and a rename
//
// Bytes are streamed to a temporary file, hashed as they go, and renamed into
// place only once complete. `rename(2)` is atomic within a filesystem, so a
// crash mid-write leaves a temp file and never a truncated blob under a name
// that promises complete contents.
//
// **The hash is computed while writing, not after.** Reading the file back to
// hash it doubles the I/O and opens a window where the bytes on disk are not the
// bytes that were hashed.
//
// # Two levels of fan-out
//
//	ab/cd/abcdef0123...
//
// A quarter of a million artifacts in one directory makes `ls` unusable and some
// filesystems slow. Two bytes of the hash gives 65,536 buckets, which is enough
// for corpora far past anything this product will hold on one disk.
//
// # Deliberately absent
//
// **Delete.** Retention is a policy about the RECORD — the mock's own table says
// artifacts are kept for the life of an engagement — and deleting bytes another
// row still cites turns a citation into a lie. Sweeping unreferenced blobs is a
// job that needs to know what references them, which this package cannot see.
//
// **Compression and encryption at rest.** Both are real and neither is free to
// add later behind the same port: a compressed blob's name is still the hash of
// the PLAINTEXT, which is a decision to write down rather than to assume.
//
// **A second hash algorithm.** [Ref] names one, so the day sha256 is not enough
// there is a field to widen rather than a convention to find.
package blob
