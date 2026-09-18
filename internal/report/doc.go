// Package report holds the engagement written up — decisions/0042.
//
// # A section is a capability
//
// `CLAUDE.md` gives this noun one line and that is the load-bearing half of it.
// Each section is generated from something the system can actually source, so
// **there is no free text and no section that says something the record cannot
// back**. That is why the section list is an enum here rather than data a person
// types, and why `report` imports nothing: seven ports, one per section, adapted
// at the composition root. A section nobody can source has no adapter and
// therefore cannot exist.
//
//	1  scope and method             what the engagement was allowed to touch
//	2  attribution evidence         why each asset is thought to be theirs
//	3  asset inventory              what was found
//	4  findings, by severity        what is wrong
//	5  coverage — what was NOT tested
//	6  invocation log, incl. refusals          OFF by default
//	7  raw artifacts appendix                  OFF by default
//
// **Six and seven are exactly the two capabilities the members matrix withholds
// from a client**, which is why they had to be two toggles and not one:
// `CLAUDE.md`'s *"generate a report vs receive its artifacts — the client gets
// one, not both"*.
//
// **Coverage cannot be turned off.** A findings report with no coverage section
// is the document every other tool produces, and the one this product exists to
// refuse: *"this section exists because a report that omits it implies a
// completeness nobody achieved."* Making it a toggle would make refusing it
// optional.
//
// # A configuration until it is issued, and then bytes
//
//	draft    sections toggle · counts render LIVE · nothing is promised
//	issued   a revision exists. The BYTES are the deliverable
//
// Issuing renders every enabled section to JSON and stores it through
// `pkg/blob` — the same machinery an artifact uses, for the same reason. The
// hash is what makes *"the client received exactly this"* checkable rather than
// asserted, and it is why a re-issue writes a SECOND revision beside the first
// rather than over it.
//
// **The configuration stays editable after issuing.** The revision is the frozen
// thing. The alternative makes a firm clone a report to change one toggle, and a
// pile of near-identical reports is worse than a revision list.
//
// **The enabled set is STORED and the numbering is DERIVED.** Defaults are
// defaults; a report configured a year ago with the invocation log on must still
// say so after the default moves. Numbers renumber over the enabled set — a
// stored one is wrong the moment a toggle moves.
//
// # Everything is read in ONE transaction
//
// A document cannot show twelve assets in one section and thirteen in another
// because a scan landed between two reads. **The report is where every count in
// the product has to reconcile**, and two sections disagreeing is a bug the rest
// of the product can hide and this one cannot.
//
// # There is no PDF
//
// The server emits structured JSON per section and the client renders. A page is
// a rendering property and this server does not render pages — `CLAUDE.md` is
// explicit that a mock is a reference for layout and never for code, and Go's
// PDF options are a heavy dependency for something a print stylesheet does. The
// mock's page counts went with it; every count this side can honestly give is
// in the document.
package report
