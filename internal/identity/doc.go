// Package identity owns accounts, credentials and sessions.
//
// It answers **who somebody is**. What they may do is org's, and enforcing it is
// the transport's. That split is the whole reason this package can be built
// first: it has no dependency on the product, and the product's every human
// record points back at a row it owns.
//
// # Owns
//
// The account, its credentials, and its sessions. An account is the identity a
// human attribution, a judgement and an audit entry all point at.
//
// **An account is never deleted, only archived** — decisions/0005. Every one of
// those rows names it, and they are the record this product exists to keep.
// Deleting it orphans them or forces a cascade that destroys evidence.
//
// # Does not own
//
// Roles and membership, which are org's. Who may see what, which is a policy the
// composition root applies. The workspace, which is a container rather than a
// permission.
//
// **Not registration.** decisions/0012 puts it above every domain: one
// transaction writes an account, a personal org and a workspace, and this
// package does not know the other two exist. pkg/postgres carries the
// transaction on the context, so this package's repository composes into a
// transaction it has never heard of, with no flag and no second method.
//
// **Not password hashing.** A Credential holds an opaque Hash string that the
// domain neither computes nor interprets. That is what lets every type here be
// tested with a literal, and it puts the choice of KDF in one place instead of
// in the type that stores its output.
//
// # An identifier is an id.ID and is not wrapped
//
// There is no AccountID type. pkg/id is the repository's answer to identity, and
// a per-domain wrapper would have to re-declare String, MarshalText and Parse to
// buy a compiler check that is weak anyway — once every id in a signature is
// sixteen bytes, the mixup a wrapper catches is the one a field name already
// prevents. Named fields carry the meaning.
//
// # The three types, and what each one refuses
//
//	Account     who somebody is        pending -> active -> archived
//	Credential  how they prove it      password | api_key
//	Session     that they proved it    a hash of a token, never the token
//
// ## Account
//
// [Email] is a value type, not a string, and normalisation happens once in
// [NewEmail]: trimmed and lowercased. **The unique index in the migration only
// means what it appears to mean while that holds** — two rows differing by case
// are two accounts for the same person, and a database cannot see that unless
// the values arrive already folded.
//
// Validation is deliberately shallow: non-empty, within 254 bytes, one @ with
// something on each side, and a dot in the domain. It is not RFC 5322 and does
// not try to be. **The only proof an address works is a message arriving at it**,
// so a stricter parser rejects deliverable addresses in exchange for a promise
// it cannot keep.
//
// [Status] is a closed set of three:
//
//	pending    created, not yet verified. CAN sign in; cannot act
//	active     verified. Full capability
//	archived   removed, cannot sign in, and still named by every record it authored
//
// **A pending account authenticates** — decisions/0018. Authentication answers
// who you are; whether you have somewhere to put things is authorisation, and
// putting it at the first gate made a typo a permanent lockout and made a stuck
// subscriber able to lock out a verified user. [Account.CanAuthenticate] is
// therefore false only for archived.
//
// The protection did not go away: it moved to the gate that resolves a caller's
// workspace, which refuses when there is none.
//
// **Archived is terminal.** An archived account does not return to active,
// because reinstating one silently reattaches every historical claim it made to
// somebody who may now be a different person. Restoring access is creating an
// account; the old one stays where it is, still naming what it did.
//
// [Status.String] and [ParseStatus] live beside each other so that renaming a
// value breaks both halves at once. A one-way mapping is how a stored string
// drifts from the constant that wrote it.
//
// ## Credential
//
// Two kinds, and they are not variants of one thing:
//
//	password   one per account. Replacing it is an update, not a second row
//	api_key    many per account, each named, each independently revocable
//
// **An api_key with no name is an api_key nobody can revoke**, so the name is
// required for that kind and optional for a password. This is the sort of rule
// that reads as cosmetic and is not: a revocation screen listing three
// indistinguishable rows is a screen where nobody revokes anything.
//
// Hash is opaque. The domain never computes, verifies or parses it, and the only
// thing it asserts is that it is non-empty — a credential with no hash is a
// credential that authenticates anybody.
//
// ExpiresAt and RevokedAt are both zero-means-never, and they are **two fields
// because they are two facts**: an expiry is a decision made when the credential
// was issued, a revocation is a decision made about it later. Collapsing them
// loses the ability to say which one ended it, which is the first question asked
// when a key stops working.
//
// [Credential.Live] is the predicate, evaluated against a supplied instant
// rather than time.Now. A predicate that reads the clock cannot be tested at a
// boundary, and every interesting case here is a boundary.
//
// ## Session
//
// A session stores **a hash of its token and never the token**. The token is
// returned once, to the caller who created it, and a stolen database therefore
// yields nothing that can be presented. This is the same rule as a password with
// a different threat model, and the reason it is stated here rather than assumed
// is that a session token is the one secret whose plaintext is genuinely
// convenient to keep.
//
// The hash is fast, not memory-hard, and that is deliberate: a session token is
// 256 random bits, so there is nothing to guess, and running a memory-hard KDF
// on every authenticated request is a denial of service you built yourself.
// The choice of function lives with the hasher, not here — see *Does not own*.
//
// A session carries the user agent and IP it was created from, for one purpose:
// **a person looking at their own session list must be able to recognise a
// session they do not recognise.** Neither field is trusted, and neither is used
// for any decision.
//
// # Events
//
//	identity.account.created     identity.account.archived
//	identity.credential.changed  identity.session.started
//	identity.session.ended
//
// **The first subscribers already exist and are not hypothetical.** A personal
// org and a workspace are provisioned at registration — decisions/0012 — and
// sessions are invalidated when a credential changes. Two consumers, neither of
// them audit, which is the argument decisions/0007 makes for an outbox over a
// direct port.
//
// **credential.changed carries no hash and no kind-specific payload.** It says
// which account's credential changed and when. A subscriber that needs more is
// asking for a secret to travel through a queue.
//
// # Storage
//
// Schema `identity`, its own migration sequence, and its ledger in that schema —
// postgres.InSchema("identity"). **No foreign key crosses out of it**, which is
// what makes extracting this package into its own service a dump of one schema
// rather than an untangling.
//
// The constraints in the migration are not belt-and-braces for the rules above;
// they are the half that survives a hand-edited row. The email-is-lowercase
// check and the api-key-is-named check exist because [NewEmail] and
// [NewAPIKey] can be bypassed by anything holding a psql prompt, and the
// unique index on a live email is the only thing that makes concurrent
// registration of the same address deterministic.
//
// # Two partial unique indexes, and the predicate is the decision
//
//	account_live_email             (email) where status <> 'archived'
//	credential_one_live_password   (account_id) where kind = 'password'
//	                                            and revoked_at is null
//
// **An archived account keeps its email row forever**, because every
// attribution it authored still names it. A *total* unique index would
// therefore burn an address for life the first time somebody left — and
// decisions/0012 says restoring access is creating an account, which a total
// index makes impossible rather than merely discouraged. So the index covers
// live rows only, and AccountByEmail carries the same predicate: the lookup and
// the constraint have to agree about what "taken" means or one of them is
// lying.
//
// **One live password per account** is stated in the domain as a rule about
// updates and enforced here as a rule about rows. Without the index it survives
// only while no two requests arrive together, which is the definition of a bug
// that appears in production and not in a test. Revoking frees the slot and
// leaves the old row where it is, so a rotation is two rows and a history
// rather than one row and a rewrite.
//
// // **Version is optimistic concurrency and starts at 1.** Two administrators
// archiving the same account concurrently is not a race worth losing a write
// over; two of them editing a credential is. The column is on every mutable row
// so the rule is uniform rather than remembered.
//
// # Errors
//
// Every failure that leaves this package is a pkg/errors value carrying a Kind,
// and **nothing here imports the standard library's errors package.** pkg/errors
// re-exports Is, As, Unwrap and Join for exactly that reason: one vocabulary,
// so a caller never has to know which of two packages produced the value it is
// holding.
//
// A repository answers in one of three shapes:
//
//	a domain sentinel, wrapped   the database refused something this package
//	                             has a word for. ErrAccountExists, ErrStaleWrite
//	a translated driver error    everything else the driver said, through
//	                             postgres.Translate, with the SQLSTATE as a detail
//	nil
//
// **A sentinel is returned wrapped, never bare** — fmt.Errorf("identity: insert
// account: %w", domain.ErrAccountExists). Wrapping with %w keeps errors.Is true
// and keeps the Kind, because fmt.Errorf produces no Error of its own and the
// sentinel stays the outermost one. Returning it bare compiles, passes every
// test that asserts with Is, and throws away the only thing a log has to say
// which of four call sites produced it.
//
// **The driver is named in one file and nowhere else.** No repository method
// mentions pgx.ErrNoRows. postgres.Translate already maps it to NotFound, so
// the adapter branches on the TRANSLATED kind rather than on the driver's
// sentinel, in one helper the three repositories share. That is safe because
// NotFound has exactly one producer in that function — no SQLSTATE maps to it —
// so "the kind is NotFound" and "the query matched no row" are the same claim.
//
// The alternative, which the previous build took, is to stop at the kind and
// have no sentinel at all. It loses a real distinction: a command that reads an
// account and then its password gets NotFound from both, and can only tell them
// apart by remembering which call it made last. pkg/errors' own contract asks
// for the sentinel — "a domain declares its own failures as sentinels built
// with New" — and this is what that buys.
//
// **Two absences are distinguished, and the distinction costs a second query.**
// A save that matches no row means either the row is gone or the version moved,
// and UPDATE ... WHERE id = $1 AND version = $2 reports both as zero rows
// affected. So a zero-row save reads the row back: absent is
// ErrAccountNotFound, present is ErrStaleWrite. A caller retries one of those
// and not the other, which is the whole reason the extra read is worth paying
// for rather than collapsing both into a conflict.
//
// # Signing in, and the three things it must not leak
//
// **A wrong password and an unknown address answer identically.** Same error,
// same shape, and the same WORK: a caller that finds no account still verifies
// against [crypto.Hasher.Dummy] and discards the result. Skipping it makes the
// endpoint a user directory readable with a stopwatch — 50ms for a real account,
// 0.2ms for a stranger — and no amount of care in the comparison closes that.
//
// **A session stores the hash of its token and never the token.** The plaintext
// is returned once, to the caller who signed in. A stolen database yields
// nothing that can be presented.
//
// **A rehash happens during verification or not at all.** [crypto.Verification]
// reports when a stored hash is below current policy, and the plaintext is in
// hand exactly once — at that moment. Deferring it means the parameters chosen
// on the first day are frozen for the life of every account.
//
// A rehash that fails does not fail the sign-in. The credential the caller
// presented was correct; refusing them because an optimisation could not be
// written would trade a correct outcome for a tidier one.
//
// # Deliberately absent
//
// **Authorisation.** This answers who somebody is.
//
// **Tokens for email verification and password reset.** They are a fourth table
// with a different lifecycle — single-use, short-lived, delivered out of band —
// and they need mail, which nothing here has. They arrive with the verification
// flow, and a `pending` account is what holds the place until then.
//
// **Sign-in, sign-out and registration commands.** They need a password hasher
// and a token minter, and neither is a row in STACK.md yet. The domain layer is
// complete without them precisely because it takes a hash rather than a
// password.
//
// **Multi-factor, SSO, and account merging.** Each is a real answer to a problem
// nothing here has. Merging in particular would have to decide what happens to
// two accounts' attributions, which is a decisions/ question and not a feature.
package identity
