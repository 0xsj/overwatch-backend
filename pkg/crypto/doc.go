// Package crypto stores and mints secrets. It is the only package that imports
// golang.org/x/crypto, and the only one that calls argon2.
//
// # Two kinds of secret, opposite treatment
//
// This is the distinction the package exists to enforce, and it is got backwards
// in both directions about equally often:
//
//	password                  low entropy, guessable   slow, memory-hard KDF
//	session token · api key   256 random bits          a fast hash
//
// A password must be expensive to test, because an attacker holding the database
// will test billions of candidates offline and the only defence is making each
// attempt cost something. A 256-bit random token has nothing to guess: there is
// no dictionary, no reuse across sites, and no human who chose it. Running
// argon2 on every authenticated request would burn 64 MiB and tens of
// milliseconds per call — **a denial of service you built yourself**, defending
// against an attack that does not apply.
//
// So [Hasher] is argon2id and [HashToken] is SHA-256, and neither is a shortcut
// for the other. Plain SHA-256 is *correct* for a high-entropy token and
// catastrophic for a password.
//
// [HashToken] returns **lowercase hex**, 64 characters. That is part of the
// contract rather than an implementation detail: the value is what a repository
// stores in a text column and what a caller compares against, so changing the
// encoding invalidates every stored row.
//
// # The hash carries its own parameters
//
//	$argon2id$v=19$m=65536,t=3,p=4$<salt>$<hash>
//
// PHC string format: algorithm, version, cost parameters and salt all travel
// with the digest. **So the parameters can be raised without invalidating a
// single existing hash** — verification uses the parameters the hash was made
// with, and [Verification.NeedsRehash] reports when those are below current
// policy.
//
// **Below, dimension by dimension** — true when ANY of memory, iterations,
// parallelism, salt length or key length is under the hasher's own. Not "differs
// from": a hash stored under STRONGER parameters must verify without asking to
// be redone, because rehashing it to policy would quietly weaken a credential,
// and doing that inside a routine sign-in is the worst possible place for it.
//
// The rule is per dimension because [Params] has no total order — a hash with
// more memory and fewer iterations is neither above nor below policy as a whole.
// Comparing a derived cost scalar would need a cost model that argon2 does not
// supply and that would be wrong the day the parameters are retuned. Per
// dimension is total, needs no model, and lands every rehash at or above policy
// everywhere.
//
// The cost is named rather than hidden: an operator who *lowers* policy — because
// the parameters were tuned too high and sign-ins were timing out — never gets
// existing expensive hashes brought down, because none of them is below the new
// floor. That is a migration, not a per-verify concern, and this is not where it
// belongs.
//
// **This was wrong until 2026-09-06**, when a suite written from this file
// against no access to the code asserted the documented rule and failed. The
// code read `params != h.params`. See STATUS.
//
// The caller rehashes at that moment, because verifying is the one moment the
// plaintext is in hand. Without it, whatever was chosen on the first day is
// frozen for the life of every account, and the only migration available is
// forcing a password reset on everybody.
//
// [Default] is RFC 9106 §4's second recommended option — t=3, p=4, m=64 MiB —
// with a 16-byte salt and a 32-byte key. Citing the RFC beats inventing numbers.
// The operational cost is real and worth stating: 64 MiB per concurrent hash, so
// ten simultaneous logins hold 640 MB transiently.
//
// # A decoder that accepts exactly what the encoder produces
//
// [Hasher.Verify] re-encodes every parsed segment and compares it to the input
// rather than trusting the parse. fmt.Sscanf does not require having consumed
// the whole string — "v=19junk" parses as 19 with no error — so a permissive
// reader accepts hashes it did not write and then behaves as though it had.
//
// The comparison is crypto/subtle.ConstantTimeCompare. A byte-by-byte compare
// leaks how much of the digest matched, through timing, to an attacker who can
// submit candidates.
//
// # Hasher.Dummy exists to stop user enumeration
//
// "No such account" must take as long as "wrong password", or the sign-in
// endpoint is a user directory that anybody can read with a stopwatch. So a
// caller that finds no account **verifies against [Hasher.Dummy] anyway** and
// discards the result.
//
// It is computed eagerly in [NewHasher] — one hash at boot — rather than lazily,
// because a lazily built dummy makes the FIRST not-found sign-in measurably
// slower than the rest, which is the leak in miniature.
//
// **This is a rule the caller has to follow and this package cannot enforce.**
// It can only make the correct thing cheap and name it here.
//
// # A token is minted and hashed in one call
//
// [Minter.New] returns both halves at once:
//
//	tok, err := minter.New()
//	// tok.Plaintext  -> the caller, once. A secret.String, so it cannot be logged
//	// tok.Hash       -> the database
//
// Returning them together is the API making the rule hard to get wrong: there is
// no call that yields a token without also yielding what to store, so "store the
// hash, return the plaintext" is the shape of the only thing available rather
// than a convention to remember.
//
// The plaintext is a [secret.String], which redacts through fmt, encoding/json
// and log/slog. A session token in a log line is the same breach as one in a
// table, and the type is what makes that unwriteable rather than merely
// discouraged.
//
// **v1 split this across two packages** — pkg/crypto for one-way functions and
// pkg/random for entropy — on the argument that generating and storing are
// different jobs. Not reproduced: the entropy source is already a constructor
// parameter here (the same injectable-reader shape as pkg/id), and one function
// does not earn a package. The split is worth revisiting the first time
// something needs randomness that is not a token.
//
// # No pepper, and it stays addable
//
// A server-side secret mixed into every hash means a stolen database is useless
// on its own. It also needs key management this project does not have, and
// losing it destroys every password irrecoverably.
//
// It stays addable precisely because of rehash-on-verify: the moment you need
// the plaintext to re-derive with a pepper is a moment you already have it.
//
// # Deliberately absent
//
// **A keyring, AEAD, HMAC, signatures, HKDF.** None has a consumer. Trigger:
// field-level encryption, or signing anything.
//
// Token hashing is deliberately unkeyed. HMAC would need a key to manage and
// buys nothing against a secret with 256 bits of entropy.
//
// **A password policy.** Length, character classes, breach-list lookups: all of
// them are decisions about people, not about cryptography, and they belong
// where the account is created. This package refuses an empty password and
// nothing else.
package crypto
