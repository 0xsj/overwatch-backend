package domain

import (
	"sort"
	"strings"
	"time"

	"github.com/0xsj/overwatch-backend/pkg/id"
)

const (
	MaxNameLength = 120
	MaxArgvLength = 2000
)

// Intensity is how loud a tool is, and it is what a scope rule qualifies:
// "a range in scope for passive collection is not thereby in scope for a loud
// scan" — 0010. The spelling matches scope's, deliberately, because a rule's
// tools list and a tool's intensity are compared as strings across a boundary
// neither package may cross.
type Intensity uint8

const (
	IntensityPassive Intensity = iota
	IntensityLight
	IntensityLoud
)

var intensityNames = map[Intensity]string{
	IntensityPassive: "passive", IntensityLight: "light", IntensityLoud: "loud",
}

func (i Intensity) String() string {
	if s, ok := intensityNames[i]; ok {
		return s
	}
	return "passive"
}

func ParseIntensity(s string) (Intensity, error) {
	for i, name := range intensityNames {
		if name == s {
			return i, nil
		}
	}
	return IntensityPassive, ErrIntensityUnknown
}

// Feed is what moves along an edge in a check's chain — decisions/0032 and 0034.
// A tool declares one it eats and one it emits, and an edge is legal when the
// upstream's Produces is the downstream's Consumes.
//
// **It is the KIND VOCABULARY plus `finding`, and that is the only difference.**
// A finding is a `CLAUDE.md` §Scope noun with its own lifecycle, explicitly *not
// an observation* — so it is not a fragment, and it is a thing bytes can be
// between two programs. That is a real superset member rather than a divergence.
//
// **There is no `domain`.** A domain is a host in a role — `0009`'s asset
// argument one level down. It was here until 2026-09-07; rows carrying it were
// rewritten to `host`, and a tool that genuinely needs a registrable domain
// checks the VALUE rather than asking for a second kind.
//
// `scope` holds the canonical copy of the fourteen and `root` holds the test
// that fails when these drift. U1 forbids the import: they are peers.
type Feed uint8

const (
	FeedNone Feed = iota
	FeedHost
	FeedCIDR
	FeedIP
	FeedASN
	FeedURL
	FeedRepo
	FeedEmail
	FeedAccount
	FeedDocument
	FeedOrg
	FeedPerson
	FeedCert
	FeedKey
	FeedWhois
	FeedFinding
)

var feedNames = map[Feed]string{
	FeedHost: "host", FeedCIDR: "cidr", FeedIP: "ip",
	FeedASN: "asn", FeedURL: "url",
	FeedRepo: "repo", FeedEmail: "email", FeedAccount: "account",
	FeedDocument: "document", FeedOrg: "org", FeedPerson: "person",
	FeedCert: "cert", FeedKey: "key", FeedWhois: "whois",
	FeedFinding: "finding",
}

// EveryFeed is the vocabulary plus `finding`, in a stable order. A function, so
// a caller cannot append to a shared slice.
func EveryFeed() []Feed {
	return []Feed{
		FeedHost, FeedCIDR, FeedIP, FeedASN, FeedURL,
		FeedRepo, FeedEmail, FeedAccount, FeedDocument, FeedOrg, FeedPerson,
		FeedCert, FeedKey, FeedWhois, FeedFinding,
	}
}

// String answers "" for FeedNone, which is what the nullable column holds. A
// zero that spells itself as a name would make "nothing upstream" and a real
// kind indistinguishable in every log and every payload.
func (f Feed) String() string { return feedNames[f] }

// ParseFeed maps "" to FeedNone, because absent is a value here: a SOURCE step
// consumes nothing, and NULL there means "nothing upstream" rather than
// "anything".
func ParseFeed(s string) (Feed, error) {
	if s == "" {
		return FeedNone, nil
	}
	for f, name := range feedNames {
		if name == s {
			return f, nil
		}
	}
	return FeedNone, ErrFeedUnknown
}

type Status uint8

const (
	StatusActive Status = iota
	StatusArchived
)

func (s Status) String() string {
	if s == StatusArchived {
		return "archived"
	}
	return "active"
}

func ParseStatus(s string) (Status, error) {
	switch s {
	case "active":
		return StatusActive, nil
	case "archived":
		return StatusArchived, nil
	default:
		return StatusActive, ErrStatusUnknown
	}
}

// Tool is a definition, never an integration — the difference is whether adding
// `subfinder` is a deploy or an insert.
//
// **OrgID, not WorkspaceID** — decisions/0031. This is a capability the firm
// owns, not a claim about a client.
type Tool struct {
	ID        id.ID
	OrgID     id.ID
	Name      string
	Intensity Intensity

	// Argv is the template, held as text and never interpreted here. pkg/execx
	// spawns argv-only and this package does not know that exists — what a
	// template means is the runner's question.
	Argv string

	// SuccessExitCodes is which exit codes mean the tool answered rather than
	// broke — decisions/0033. It defaults to {0} and is {0, 1} for nuclei,
	// which exits 1 when it finds nothing. Empty is normalised to {0} rather
	// than refused: a tool that can only ever fail is nobody's intent.
	SuccessExitCodes []int

	// Consumes is zero on a SOURCE tool — one seeded from the target's scope
	// rather than fed by another tool. Zero is "nothing upstream", never
	// "anything".
	Consumes Feed
	Produces Feed

	Status     Status
	Version    int
	CreatedBy  id.ID
	CreatedAt  time.Time
	UpdatedAt  time.Time
	ArchivedAt time.Time
}

func New(newID, org, createdBy id.ID, name, argv string, intensity Intensity,
	consumes, produces Feed, success []int, at time.Time) (Tool, error) {
	if newID.IsZero() || createdBy.IsZero() {
		return Tool{}, ErrIDRequired
	}
	if org.IsZero() {
		return Tool{}, ErrOrgRequired
	}
	if at.IsZero() {
		return Tool{}, ErrTimeRequired
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return Tool{}, ErrNameRequired
	}
	if len(name) > MaxNameLength {
		return Tool{}, ErrNameTooLong
	}
	argv = strings.TrimSpace(argv)
	if argv == "" {
		return Tool{}, ErrArgvRequired
	}
	if len(argv) > MaxArgvLength {
		return Tool{}, ErrArgvRequired
	}
	return Tool{
		ID: newID, OrgID: org, Name: name, Argv: argv, Intensity: intensity,
		Consumes: consumes, Produces: produces, SuccessExitCodes: successCodes(success),
		Status: StatusActive, Version: 1, CreatedBy: createdBy,
		CreatedAt: at, UpdatedAt: at,
	}, nil
}

func (t Tool) Archived() bool { return t.Status == StatusArchived }

// Update changes the argv template or the intensity. The NAME is not editable
// here for no deep reason — it is, via the same call — but changing intensity is
// the one that matters: it is what every scope rule qualifies, so raising it can
// take a tool out of scope everywhere at once.
func (t Tool) Update(argv string, intensity Intensity, consumes, produces Feed,
	success []int, at time.Time) (Tool, error) {
	if at.IsZero() {
		return t, ErrTimeRequired
	}
	if t.Archived() {
		return t, ErrArchived
	}
	argv = strings.TrimSpace(argv)
	if argv == "" {
		return t, ErrArgvRequired
	}
	next := t
	next.Argv = argv
	next.Intensity = intensity
	next.Consumes = consumes
	next.Produces = produces
	next.SuccessExitCodes = successCodes(success)
	next.Version = t.Version + 1
	next.UpdatedAt = at
	return next, nil
}

func (t Tool) Archive(at time.Time) (Tool, error) {
	if at.IsZero() {
		return t, ErrTimeRequired
	}
	if t.Archived() {
		return t, ErrArchived
	}
	next := t
	next.Status = StatusArchived
	next.ArchivedAt = at
	next.UpdatedAt = at
	next.Version = t.Version + 1
	return next, nil
}

const (
	EventToolAdded    = "tool.added"
	EventToolUpdated  = "tool.updated"
	EventToolArchived = "tool.archived"
	EventMappingAdded = "tool.mapping.added"
	EventMappingLive  = "tool.mapping.promoted"

	// The subject is the ORG, not the tool — decisions/0024 reads it to make the
	// entry org-scoped, which is where a firm's own history belongs. A tool
	// event has no workspace to be tenanted to.
	SubjectKind = "org"
)

type ToolAdded struct {
	ToolID    string `json:"tool_id"`
	OrgID     string `json:"org_id"`
	Name      string `json:"name"`
	Intensity string `json:"intensity"`
}

type ToolUpdated struct {
	ToolID string `json:"tool_id"`
	OrgID  string `json:"org_id"`
	From   string `json:"from"`
	To     string `json:"to"`
}

type ToolArchived struct {
	ToolID string `json:"tool_id"`
	OrgID  string `json:"org_id"`
	Name   string `json:"name"`
}

type MappingAdded struct {
	MappingID string `json:"mapping_id"`
	ToolID    string `json:"tool_id"`
	OrgID     string `json:"org_id"`
	Field     string `json:"field"`
	Version   int    `json:"version"`
	ByPerson  bool   `json:"by_person"`
}

type MappingPromoted struct {
	MappingID string `json:"mapping_id"`
	ToolID    string `json:"tool_id"`
	OrgID     string `json:"org_id"`
	Field     string `json:"field"`
	From      int    `json:"from_version"`
	To        int    `json:"to_version"`
}

// Succeeded is the question a run asks and the reason this list is per tool.
// A tool with no codes recorded is read as {0} rather than as "nothing
// succeeds", so a row written before decisions/0033 answers the same as one
// written after it.
func (t Tool) Succeeded(exit int) bool {
	if len(t.SuccessExitCodes) == 0 {
		return exit == 0
	}
	for _, code := range t.SuccessExitCodes {
		if code == exit {
			return true
		}
	}
	return false
}

// successCodes normalises the empty list to {0}. The schema refuses an empty
// array and this is where a caller's `[]` stops being one — a constraint
// violation for a value with an obvious meaning is a bad error message.
func successCodes(in []int) []int {
	if len(in) == 0 {
		return []int{0}
	}
	out := make([]int, 0, len(in))
	seen := map[int]bool{}
	for _, code := range in {
		if seen[code] {
			continue
		}
		seen[code] = true
		out = append(out, code)
	}
	sort.Ints(out)
	return out
}
