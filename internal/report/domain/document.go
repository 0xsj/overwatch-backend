package domain

// Document is what gets FROZEN — decisions/0042 §3. It is rendered once, stored
// through `pkg/blob`, and never regenerated: the hash is what makes "the client
// received exactly this" a checkable claim rather than an assurance.
//
// It is JSON and not a PDF. A page is a rendering property and this server does
// not render pages — `CLAUDE.md` is explicit that a mock is a reference for
// layout and never for code, and Go's PDF options are a heavy dependency for
// something a print stylesheet does.
type Document struct {
	ReportID    string `json:"report_id"`
	WorkspaceID string `json:"workspace_id"`
	TargetID    string `json:"target_id"`

	Title      string `json:"title"`
	PreparedBy string `json:"prepared_by,omitempty"`

	PeriodStart string `json:"period_start,omitempty"`
	PeriodEnd   string `json:"period_end,omitempty"`

	Revision int    `json:"revision"`
	IssuedAt string `json:"issued_at"`

	Sections []RenderedSection `json:"sections"`

	// Withheld names the capability sections LEFT OUT of this document, by
	// title. It is in the document rather than computed from `Sections` because
	// a reader holding only these bytes must be able to see what is missing —
	// which is the whole argument for section 5 applied to the document itself.
	Withheld []string `json:"withheld,omitempty"`
}

// RenderedSection is one section as it was frozen. **A disabled section is
// ABSENT**, not present and empty: an empty section reads as "we looked and
// there was nothing", which is the pair `CLAUDE.md` refuses to collapse.
type RenderedSection struct {
	// Number is derived over the ENABLED set at render time and never stored.
	// Disable the second section and the rest renumber.
	Number int    `json:"number"`
	Key    string `json:"key"`
	Title  string `json:"title"`

	// Counts are what the screens draft renders beside a toggle — `14 assets ·
	// 11 in scope, 3 not`. They are computed from the SAME reads that fill
	// `Content`, so the two cannot disagree.
	Counts []Count `json:"counts"`

	// Warning is carried into the document for the two sections that have one,
	// and it is empty here for a section that is PRESENT — a warning names what
	// omitting it costs, and this one was not omitted. It appears in `Withheld`
	// instead.
	Content any `json:"content"`
}

type Count struct {
	Label string `json:"label"`
	Value int    `json:"value"`
}

// The seven content shapes, in this package's OWN vocabulary. `report` imports
// nothing — seven ports, adapted at the composition root, which is where every
// peer in this system meets every other.

// Rule is one scope rule as the method section cites it. A SUPERSEDED rule is
// still cited: `0030` keeps it forever precisely so a citation made at the time
// still resolves, and a report that hid it would break the only reason it was
// kept.
type Rule struct {
	Pattern    string `json:"pattern"`
	Polarity   string `json:"polarity"`
	Gate       string `json:"gate"`
	Intensity  string `json:"intensity,omitempty"`
	Superseded bool   `json:"superseded"`
}

// Claim is one attribution. `Confidence` is a POINTER because only a model
// carries one — 0004 and 0003 — and `null` would make a rule's category look
// like a probability of zero.
type Claim struct {
	Fragment   string   `json:"fragment"`
	Kind       string   `json:"kind"`
	Claimant   string   `json:"claimant"`
	Confidence *float64 `json:"confidence,omitempty"`
	Basis      string   `json:"basis,omitempty"`
	State      string   `json:"state"`
}

// Asset is a fragment in a role — 0009. `InScope` is the claim gate's answer and
// is why the count partitions: `14 assets · 11 in scope, 3 not`.
type Asset struct {
	Kind     string `json:"kind"`
	Value    string `json:"value"`
	InScope  bool   `json:"in_scope"`
	LastSeen string `json:"last_seen,omitempty"`
}

// Finding excludes DISMISSED ones from the section but not from the counts — the
// draft's `8 open · 1 dismissed and excluded` is one sentence saying both.
type Finding struct {
	Signature string `json:"signature"`
	Severity  string `json:"severity"`
	State     string `json:"state"`
	Fragment  string `json:"fragment"`
	Sightings int    `json:"sightings"`
	LastSeen  string `json:"last_seen,omitempty"`
}

// Cell is one coverage cell: `never`, `stale` or `fresh`.
//
// **There is no `n/a`** — `0011`, and `entity`'s own coverage query says so out
// loud: inapplicability is the ABSENCE of a cell, not a fourth value, because a
// fourth value invites somebody to count it. So every cell here IS an applicable
// pair and the denominator needs no filtering.
type Cell struct {
	Asset string `json:"asset"`
	Check string `json:"check"`
	State string `json:"state"`
	At    string `json:"at,omitempty"`
}

// Invocation is the scope proof. `Refusal` and `RefusalRule` are what make it
// one — the record of what did NOT run.
type Invocation struct {
	Tool        string   `json:"tool"`
	State       string   `json:"state"`
	Argv        []string `json:"argv"`
	Refusal     string   `json:"refusal,omitempty"`
	RefusalRule string   `json:"refusal_rule,omitempty"`
	// Candidates are the per-thing scope proof — 0039. A step can be `ok` while
	// a rule kept it off part of what it was pointed at, and a report omitting
	// that under-reports every batched run.
	Permitted int `json:"permitted"`
	Refused   int `json:"refused"`
}

// Note is one thing a person wrote — decisions/0043. It carries its AUTHOR and
// when, which is what makes this a sourced section rather than free text: the
// claim is "this person said this, then", and the record backs it.
type Note struct {
	Body   string `json:"body"`
	Author string `json:"author"`
	At     string `json:"at"`
	// Edited says the text has changed since it was written. A report reader
	// seeing a note dated March with an April edit is entitled to know.
	Edited bool `json:"edited"`
}

// Artifact is the replay appendix. The BYTES are not here — the hash is, which
// is what a reader re-derives from.
type Artifact struct {
	Stream    string `json:"stream"`
	Hash      string `json:"hash"`
	Bytes     int64  `json:"bytes"`
	MediaType string `json:"media_type,omitempty"`
	Truncated bool   `json:"truncated"`
}
