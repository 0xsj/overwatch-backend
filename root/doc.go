// Command server is the composition root: the one place allowed to know two
// vocabularies at once.
//
// Everything below it declares the narrow interface it needs and never names a
// sibling. The import checks enforce that (U1, U2), so this file is not a
// convention — it is the only place a cross-domain operation can legally live.
//
// # Registration is NOT here, and no adapter for it is either
//
// decisions/0017 made registration a CHAIN. identity writes an account and
// publishes a fact; org provisions from that fact and publishes its own;
// workspace provisions from org's. Three transactions, three schemas, and no
// code anywhere that holds two of the three vocabularies at once.
//
// **The `tenancy` adapter this file used to describe was deleted.** It was a
// cross-domain WRITE living at the root under U2's licence, and while that was
// legal it made the three domains inseparable in one transaction — which the
// documents were simultaneously claiming could be split with a pg_dump. What
// stands in its place is three subscribers in the list below.
//
// The window it opened is real and invisible: for a few milliseconds an account
// exists with no org. Nothing can reach it, because a pending account cannot
// reach a product write path and the capability gate refuses a caller with no
// workspace — decisions/0018.
//
// # What IS here is the composed READ
//
// `GET /v1/me` — see me.go. It answers "who am I, and where may I go", and the
// second half is org's and workspace's vocabulary. It is a join of three
// ANSWERS rather than of three tables: identity says who the caller is, org says
// which orgs they are a live member of, and workspace lists the workspaces of
// each org the membership already authorised.
//
// **That is the licence U2 grants, spent on a read rather than a write.** A read
// composed here can be pulled apart into three client calls the day these become
// three services; a write composed here could not.
//
// # There is no bus, because the dispatcher already is one
//
// pkg/events' contract refuses a registry — *"delivery decides who gets what,
// and delivery is not here"* — which reads like an instruction to build a
// fan-out here. It is not: [outbox.Config] takes `Handlers []events.Handler` and
// hands each event to every one of them, with a panicking handler contained so
// one subscriber's bug cannot stop delivery for the rest.
//
// So subscribing is appending to that slice, and this file is where the list
// lives. **A fan-out written here was deleted on sight of it** — see STATUS.
//
// **Fan-out is safe only because both subscribers are idempotent by event id.**
// A handler returning an error causes the whole event to be retried, so audit
// will see an event the journal failed on a second time; its unique index turns
// that into a no-op. A subscriber added to this list that is not idempotent
// breaks the guarantee silently, which is why the property is stated rather
// than assumed.
//
// # /v1 is a namespace, and not a compatibility promise
//
// Every product route is under `/v1`. What that does and does not commit to is
// worth stating, because the prefix is usually read as a promise nobody keeps.
//
//	IT MEANS       within /v1, responses are ADDITIVE ONLY. No field is
//	               removed, renamed or retyped, and no route changes meaning
//	IT DOES NOT    that two major versions run side by side. There is one
//	MEAN           client, deployed with this server, and no plan to serve a
//	               lagging one
//
// **A breaking change gets a new path for the endpoints that break** — never a
// whole-API bump, and never an edit in place. Editing `/v1` on a breaking change
// is worse than having no prefix at all, because a client trusted the prefix and
// nothing told it otherwise.
//
// **The prefix is kept because the URL is the expensive thing to change later**
// — every client file, every saved request, every document — and it costs
// nothing now. It is insurance with a stated excess, not a versioning strategy.
//
// **What actually breaks a client is response shape, not the URL.** That is why
// the discipline above is about fields, and why STACK.md puts the wire envelope
// behind a single client file permitted to read a wire key by name. The
// containment does more real work than the prefix does.
//
// Media-type versioning — `Accept: application/vnd.overwatch.v1+json` — is more
// correct on paper, unusable with curl, and buys nothing while there is one
// client that ships with the server. Refused rather than overlooked.
//
// Adding workspace scoping is **additive**: `/v1/workspaces/{id}/targets` is a
// new route, not a changed one, so ALIGNMENT.md's request costs no bump.
//
// # The default workspace is named and the name is ordinary
//
// decisions/0017: an account that can ACT has a workspace, and the provisioned
// one is an ordinary row — no is_default, no is_personal. It is named so it is
// never rendered blank, and renaming it is the same operation as renaming any
// other. ALIGNMENT.md asks the client for a stepper that names it at signup;
// this does not wait for one.
//
// # Mail is constructed at boot and fails there
//
// [mail.New] refuses to build without a BASE_URL, so a deployment that would
// mail links pointing at the wrong host does not start. That is the failure mode
// worth killing the process for: the link arrives, looks correct, and does
// nothing, and nobody involved can tell why.
//
// BASE_URL is the **client's** origin and not this server's — a verification
// link is clicked in a browser and lands on a screen.
package root
