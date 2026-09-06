// Package provenance answers "what caused this, and what caused that" for every
// unit of work.
//
// A record that says what failed is half an answer. What the process was doing
// when it failed, which chain of work that belonged to, what opened the chain,
// and how many hops back the origin is — those travel on the context and are
// stamped onto every log line, event and audit row without a call site being
// told to pass them.
//
// # The ledger
//
//	request        this one unit of work. Minted here, never adopted
//	correlation    the chain's root — everything descending from one origin
//	causation      the immediate parent. Correlation is the tree, this is the edge
//	origin         WHY the chain exists: a request, a schedule, a replay, a backfill
//	depth          hops from the root. A bound, not a statistic
//	attempt        which delivery of this unit. 1 on the first
//	actor          WHO caused it
//	on_behalf_of   whose account it affects, when those differ
//	tenant         whose data it touches
//	traceparent    W3C, observability only, adopted and never minted
//
// # Correlation is the tree, causation is the edge
//
// This is the pair people conflate, and conflating them is the failure that
// cannot be repaired after the fact. With correlation alone you have a bag of
// records sharing an identifier and no parentage — you cannot tell whether an
// alert fired because of a scanner transcript or because of a correlation match
// that happened later in the same chain. Causation is what makes it a tree, and
// a tree is what makes "why did this fire" answerable.
//
// At an origin the correlation IS the root's request id — no second identifier
// is minted, because a chain's root and the chain are the same thing named
// twice. So [Provenance.Root] is `depth == 0 && correlation == request`, and the
// second half is not redundant: after [Provenance.Adopt] takes an upstream
// correlation, a depth-0 value is **no longer a root**. It opened this process's
// work; it did not open the chain. That distinction is unavailable to a design
// that mints correlation separately, and it is the reason this one does not.
//
// Overwatch is a pipeline — collect, ingest, entity, attribute, judge, alert —
// and every hop is a subscriber. A subscriber takes correlation from the
// envelope, causation from the EVENT's identifier, and mints a fresh request.
// Minting a new correlation instead breaks the chain at that hop, silently, and
// the break is invisible until somebody asks a question that crosses it.
//
// # Nothing branches on provenance, except the two things that do
//
// It is metadata stamped onto records, never a decision input. That single fact
// is why its rules differ from an authorization check's: a forgotten authz check
// is a security failure, while a missing correlation degrades observability and
// grants nothing. So it may travel ambiently on a context, and an adopted value
// that does not parse is dropped rather than refused — refusing the request
// would take something away for no gain, and a broken trace link is the cheaper
// failure.
//
// [Provenance.Depth] and [Provenance.Attempt] are the exception. They exist to
// be branched on, and **being the exception is exactly why neither is
// adoptable**: [Adopted] has no field for either, so a caller cannot reset a
// bound it is subject to. That is the compiler enforcing the rule rather than a
// comment asking.
//
// # Mint at an origin, derive everywhere else
//
//	New          an origin: an inbound request, a scheduled sweep, a replay
//	Adopt        an inbound boundary, once, immediately after New
//	Derive       a child caused by this unit of work
//	DeriveFrom   a child caused by something else — an event, a task, a message
//	Retry        the same unit again. Everything identical but attempt
//
// [Provenance.DeriveFrom] is the one that matters in a pipeline, and it takes a
// cause rather than an envelope because a broker consumer, a scheduled task and
// a webhook redelivery all derive identically from parent plus cause. Events are
// a higher layer, so the import would be forbidden in any case.
//
// [Provenance.Retry] is not a derive. A redelivery is the same unit of work
// arriving again, so request, correlation, causation and depth are unchanged and
// only attempt moves. Deriving instead would make a poison message look like a
// widening tree.
//
// # Depth is a bound, not a statistic
//
// An event-driven pipeline can form a cycle: a judgement raises an event, the
// event re-enters attribution, attribution raises a judgement. Every record in
// that cycle shares one correlation **by design**, so no amount of inspecting
// identifiers detects it. The hop count does.
//
// Root is depth 0. Each derive adds one. Past [MaxDepth] a derive fails with
// [ErrDepthExceeded] rather than returning a value, because a chain that deep is
// a cycle in every case anyone has produced, and returning a usable value there
// makes the runaway cheaper to continue than to stop.
//
// This is the field that turns provenance from observability into a control, and
// it is why depth is never taken from a caller: a value that bounds the work a
// caller can cause cannot also be supplied by that caller.
//
// # Attempt distinguishes a retry from a fan-out
//
// Without it, a message redelivered nine times and a message that fanned out to
// nine children are the same shape in a log: nine records, one correlation. The
// first is a poison message burning budget and the second is the system working.
//
// Attempt is 1 at the origin, not 0. "Attempt 1" is what an operator reading a
// dashboard means by the first try, and an off-by-one in the field everybody
// reads during an incident is a bad trade for arithmetic convenience.
//
// [Provenance.Retry] **saturates** rather than wrapping. At the maximum it stops
// counting and stays there; it does not return to 0. A message redelivered
// 65,536 times is a message in a state nobody needs a precise number for, and
// wrapping would report it as a first attempt — the single most misleading value
// the field can hold, arriving exactly when somebody is looking at it.
//
// [Provenance.Derive] resets attempt to 1, because a child is a new unit of
// work. Only [Provenance.Retry] moves it.
//
// # Origin is why the chain exists, not who started it
//
// [Kind] answers who — a user, a service, this system. [Origin] answers why
// there is a chain at all, and they are different questions:
//
//	OriginRequest    somebody asked for it
//	OriginSchedule   a timer fired
//	OriginReplay     a human re-ran something deliberately
//	OriginBackfill   bulk historical work, expected to be large and slow
//	OriginStartup    the process booting
//
// This matters here more than it would elsewhere, because coverage is one of the
// product's own nouns. "This target was scanned" means something different when
// the chain was a nightly sweep than when somebody clicked a button, and a
// coverage claim that cannot say which is a claim nobody can act on.
//
// A redelivery is deliberately NOT an origin. It does not open a chain; it
// continues one, with [Provenance.Retry].
//
// [OriginUnknown] is the zero value and [New] refuses it, as it refuses any
// value outside [Origins]. A chain whose reason is unrecorded is the one case
// where a default would be silently wrong for every reader, so the zero value
// exists to be un-defaultable rather than to be returned — the fail-closed half
// of [[zero-values-and-fail-closed]] is about what a zero value MEANS when read,
// not about handing one back.
//
// # What panics, and what returns an error
//
// The split is whether a caller's input could have caused it.
//
//	panics    a nil Minter · OriginUnknown or an origin outside Origins ·
//	          deriving, retrying or adopting on the zero Provenance
//	errors    everything else, and always errors.Internal
//
// A panic here is the same judgement logger.New makes about a nil Clock: no
// environment can produce it and no operator can fix it, so failing at
// construction beats failing on the first record. An origin is a literal at a
// call site; a Minter is wired in the composition root. Both are wrong at
// compile time in every sense but the compiler's.
//
// Returning an unusable zero value instead would be worse than either. It moves
// the crash to [NewContext], which is the next thing anybody does with it — so
// the process still dies, one stack frame further from the mistake, with a
// message about a context rather than about the origin nobody set.
//
// [Provenance.MarshalJSON] on the zero value is an error rather than a panic,
// because it is reached from encoding/json rather than from a call site, and
// because there is a caller-shaped mistake behind it: a stored record that
// unmarshalled into nothing. It cannot be allowed to succeed. The bytes it would
// write claim a request whose identifier is nil and whose attempt is 0, and this
// document says attempt is never 0 — a record that contradicts an invariant is
// worse than a refusal, because it is the refusal's evidence written down as
// fact.
//
// # Identity is two types, and the type is the enforcement
//
//	minted    id.ID    16 bytes, UUIDv7. Time-ordered, comparable, and
//	                   ID.Time recovers when the scope opened
//	adopted   string   a caller's bytes. Validated, untrusted, no time
//
// Our identifiers are our own format, so a value adopted at a boundary must
// parse as one — [id.Parse] or it is dropped. A foreign system that does not
// speak our format cannot join our chain, and that is correct: adopting a
// stranger's correlation makes the grouping key of our audit trail
// caller-controlled. Cross-vendor correlation is what traceparent is for, and it
// is carried verbatim precisely because nothing here reads it.
//
// [Adopted] has fields for correlation, causation and traceparent, and **none
// for request, actor, tenant, depth or attempt**. So "adopt what can only
// correlate, never what confers identity, authority or a bound" is a fact about
// the type rather than a rule somebody has to remember.
//
// # Delegation names both parties, and refuses three shapes
//
// [Provenance.Actor] is who really did it. [Provenance.OnBehalfOf] is whose
// account it affects. Both travel on every record, because a trail that records
// an operator's action as the user's is not incomplete — it is **false**, in the
// one table whose entire value is that it is not.
//
// [Provenance.WithOnBehalfOf] returns an error in three cases, and each is a
// record that would be worse than no record:
//
//	the delegate is anonymous     acting for nobody is not a delegation
//	the actor is anonymous        nobody cannot act for somebody
//	they are the same actor       acting for yourself is not a delegation either
//
// The zero [Actor] is the anonymous actor: absent and anonymous are the same
// value on purpose, because a record with no actor and a record by nobody are
// the same claim. For the same reason [User], [Service] and [System] refuse to
// build an actor of kind [KindAnonymous] — a *named* anonymous actor would make
// that identity ambiguous, and [Anonymous] is the only way to obtain one.
//
// It is not adoptable. [Adopt] takes correlation, causation and traceparent from
// a caller; letting a caller also claim to be acting for somebody would make the
// field worthless. Like the actor, it is set after authentication.
//
// # A stored record is validated on the way back
//
// [Provenance.UnmarshalJSON] refuses what [New] and the derive family could
// never have produced: a request id that is not an identifier, a depth past
// [MaxDepth], an attempt of 0, a tenant outside the charset, a malformed
// traceparent, and an origin that is absent, unrecognised, or [OriginUnknown].
// These are our own bytes, so a violation is corruption rather than input, and
// it reports errors.Internal.
//
// The origin case is stricter than it first looks, and deliberately. `unknown`
// is a name [Origin.String] produces and [ParseOrigin] therefore accepts — but
// [New] refuses it, so no record this package wrote can carry it. Reading one
// back would accept a value, store it, and then omit the key on the way out,
// collapsing *absent* and *explicitly unknown* into one state and losing the
// value silently. The reader checks membership in [Origins], the same predicate
// the constructor checks, because a rule enforced at one end of a round trip and
// not the other is not enforced.
//
// Accepting them would be worse than failing. A record whose depth exceeds the
// bound has escaped the cycle control; one whose attempt is 0 contradicts a rule
// stated in this document. Reading either back as valid launders a broken record
// into a trusted one.
//
// # Immutable, constructed only, and comparable
//
// Every field is unexported and there is no struct literal. The only ways to
// obtain a [Provenance] are [New], [Provenance.Derive], [Provenance.DeriveFrom]
// and [Provenance.Retry] — because hand-building is how a parent's request
// becomes its child's, which collapses the causal graph into a self-loop that
// reads as a valid record.
//
// It is comparable, so it works as a map key and `==` means what it looks like.
// That constrains the design: no slice or map field, ever, whatever arrives
// later. A field that needs one belongs on the record, not on the provenance.
//
// # Errors are Internal, not Invalid
//
// Every failure here reports errors.Internal. An actor comes from the
// authenticator or a literal in this repository; a cause is an identifier this
// system minted; a stored record is our own bytes. Nobody can fix any of them by
// sending different input, and reporting Invalid would put a 400 on a fault that
// is ours.
//
// The one place a caller's bytes arrive is [Provenance.Adopt], which returns no
// error at all — it takes each field independently and drops what it cannot use.
//
// # The log fields are a contract
//
// [FieldRequest] and its siblings are referenced by everything that writes a
// record, so no two components disagree. A component emitting request_id while
// another emits request passes its own tests while the corpus drifts apart, and
// nothing fails anywhere — the same argument as logger.FieldErrKind.
//
// [Provenance.Attrs] returns them FLAT: request_id at the top level of the
// record, not nested under a provenance key, because that is the shape a query
// wants. [Provenance.LogValue] exists for the case where one is logged as a
// value anyway, since unexported fields would otherwise render as nothing.
//
// An absent field is omitted. It is never emitted as an empty string or a null:
// request_id="" is a claim that a request existed and had no identity, which is
// not a state this package can produce. Absent, empty and defaulted are three
// states here for the same reason they are three in pkg/env.
//
// # Identifiers, and the injection surface
//
// An adopted string is one to [MaxIDLength] characters of A-Z a-z 0-9 and
// . _ : / - — narrower than printable ASCII on purpose. It accepts every format
// anyone actually emits and rejects quotes, backslashes, angle brackets, control
// characters and whitespace, which is the log-injection and header-echo surface.
//
// This is a security control rather than hygiene. A value adopted from a header
// and echoed into a log line can forge log entries with a newline, and can drive
// a terminal with an escape sequence — and this is the one record whose value is
// that it can be trusted.
//
// traceparent gets its own shape check, because the general rule would accept a
// malformed one and a tracing backend then discards the span with nothing to
// point at. **The shape is stated here rather than cited**, because a reader
// deriving tests from this document may not be able to go and look it up, and a
// rule that cannot be read is a rule that cannot be tested:
//
//	00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01
//	vv ^^^^^^^^^^^^ trace id, 32 ^^^^^^ ^ parent id, 16 ^ ^^ flags
//
//	exactly 55 characters, and hyphens at index 2, 35 and 52
//	every other byte is LOWERCASE hex — 0-9 a-f, never A-F
//	version ff is forbidden; it is reserved by the standard
//	an all-zero trace id or an all-zero parent id is invalid
//
// The last two are the ones a length-and-charset check misses, and they are the
// two a caller sending a placeholder actually produces.
//
// # What is not provenance
//
// **Extraction metadata is domain data.** Which source, which collector run,
// human or model, which model, which prompt version, what confidence, who
// corrected it — those describe the observation, not the request that recorded
// it, and they belong on the record.
//
// The pull is to put "which model" on the [Actor], because it reads as who did
// this. Resist it: a runtime module carrying model version strings is a boundary
// that does not come back, and the correction workflow needs that data queryable
// on the observation anyway. The actor for an extraction is System("extract/llm").
//
// The boundary is sharper here than in most systems, because lineage — an
// observation's path back to the raw bytes a tool emitted — is the product. Two
// systems that answer "how do I know this" would each answer half. They link in
// one direction and one field: a lineage record stores the correlation of the
// run that produced it, which is why these values must be storable and
// comparable in the domain and not merely loggable.
//
// Note also that an unauthenticated webhook's actor is our own receiving path,
// not the claimed sender. Service("pulsepoint") is an identity claim, and
// identity claims need authentication.
//
// # Deliberately absent
//
// A sampling decision. It belongs to whatever exports spans, and there is
// nothing to export to.
//
// A deadline or budget. Cancellation is context's own job and it already does it
// — see [[cancellation-is-a-race-not-a-flag]].
//
// Anything read from an environment variable. This package is handed what it
// needs; reading configuration deep in a tree is the dependency nothing
// announced.
//
// A middleware, and any knowledge of a transport. Opening a scope at an HTTP
// edge is two calls, and they belong at the edge — the collector and the tool
// runner open theirs with the same calls and have no transport at all.
package provenance
