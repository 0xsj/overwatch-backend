package domain

import (
	"sort"
	"strings"
	"time"

	"github.com/0xsj/overwatch-backend/pkg/id"
)

const (
	MaxNameLength     = 120
	MaxQuestionLength = 400
)

// Subject is what a coverage question is asked ABOUT, and it is EXACTLY the
// TARGETABLE FACET of the kind vocabulary — decisions/0034 and 0009.
//
// Coverage is over ASSETS, and `0009` defines an asset as a fragment whose kind
// is targetable carrying an accepted attribution. So this list is not a separate
// vocabulary and must not drift into one: it is seven of `scope`'s fourteen, and
// `root` holds a test that fails the moment it is not.
//
// **This is NOT tool.Feed.** A feed is what bytes can be as they move between
// two programs and it includes `finding`; a subject is a thing an engagement can
// be incomplete about, and a finding is not one — folding it in makes the
// coverage ratio mean two things at once.
//
// **There is no `domain`.** A domain is a host in a role — `0009`'s argument
// about assets, applied one level down. It was here until 2026-09-07 and rows
// carrying it were rewritten to `host`.
type Subject uint8

const (
	SubjectHost Subject = iota
	SubjectCIDR
	SubjectIP
	SubjectASN
	SubjectURL
	SubjectRepo
	SubjectEmail
)

var subjectNames = map[Subject]string{
	SubjectHost: "host", SubjectCIDR: "cidr", SubjectIP: "ip",
	SubjectASN: "asn", SubjectURL: "url",
	SubjectRepo: "repo", SubjectEmail: "email",
}

func (s Subject) String() string {
	if n, ok := subjectNames[s]; ok {
		return n
	}
	return "host"
}

func ParseSubject(s string) (Subject, error) {
	for k, name := range subjectNames {
		if name == s {
			return k, nil
		}
	}
	return SubjectHost, ErrSubjectUnknown
}

// EverySubject is what the human check applies to — 0011 requires `READ BY YOU`
// to be universal, because a person can read anything. It is a function rather
// than a package variable so a caller cannot append to the shared slice.
func EverySubject() []Subject {
	return []Subject{
		SubjectHost, SubjectCIDR, SubjectIP, SubjectASN, SubjectURL,
		SubjectRepo, SubjectEmail,
	}
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
		return StatusActive, ErrSubjectUnknown
	}
}

// Check is a named question with its own interval.
type Check struct {
	ID    id.ID
	OrgID id.ID

	Name string

	// Question is the sentence a person reads, and it is stored SEPARATELY from
	// the chain because coverage counts questions rather than tools. Rewiring a
	// chain leaves the question — and therefore the coverage cell — the same.
	Question string

	// AppliesTo is the applicability matrix decisions/0011 created and did not
	// fill in. Non-empty: a check that applies to nothing has no cell anywhere
	// and is a row that can only ever be wrong.
	AppliesTo []Subject

	// Interval is ZERO for "when somebody asks", which is a kind of check and
	// not an unset field — the human check has no clock and never goes stale.
	Interval time.Duration

	Enabled bool

	// Human says a PERSON reading this is the whole act — `READ BY YOU`. It is a
	// FLAG and not "has no chain", because a check nobody has wired a chain to
	// yet is also chainless, and deriving it made an unfinished check report
	// coverage it did not have — decisions/0037 §3.
	Human bool

	Status     Status
	Version    int
	CreatedBy  id.ID
	CreatedAt  time.Time
	UpdatedAt  time.Time
	ArchivedAt time.Time
}

// Draft is what a caller supplies. It carries no chain: the steps are written
// against a check that already exists, because a step names a tool and a flow
// names two steps, and neither has an id before the insert.
type Draft struct {
	Name      string
	Question  string
	AppliesTo []Subject
	Interval  time.Duration
	Enabled   bool
	Human     bool
}

func New(newID, org, createdBy id.ID, in Draft, at time.Time) (Check, error) {
	if newID.IsZero() || createdBy.IsZero() {
		return Check{}, ErrIDRequired
	}
	if org.IsZero() {
		return Check{}, ErrOrgRequired
	}
	if at.IsZero() {
		return Check{}, ErrTimeRequired
	}
	name, question, applies, err := clean(in)
	if err != nil {
		return Check{}, err
	}
	if in.Interval < 0 {
		return Check{}, ErrIntervalNegative
	}
	return Check{
		ID: newID, OrgID: org, Name: name, Question: question,
		AppliesTo: applies, Interval: in.Interval, Enabled: in.Enabled,
		Human:  in.Human,
		Status: StatusActive, Version: 1, CreatedBy: createdBy,
		CreatedAt: at, UpdatedAt: at,
	}, nil
}

func (c Check) Archived() bool { return c.Status == StatusArchived }

// OnDemand is the check with no clock. It reads better than `Interval == 0` at
// the call sites that care, and there are two: a scheduler that must not pick it
// up, and a coverage cell that must never report `stale` for it.
func (c Check) OnDemand() bool { return c.Interval == 0 }

// Applies answers whether this check produces a cell for a subject. A `false`
// here is `n/a` — NOT a pair, excluded from both the numerator and the
// denominator — and it is deliberately not the same value as "never checked".
func (c Check) Applies(to Subject) bool {
	for _, s := range c.AppliesTo {
		if s == to {
			return true
		}
	}
	return false
}

func (c Check) Update(in Draft, at time.Time) (Check, error) {
	if at.IsZero() {
		return c, ErrTimeRequired
	}
	if c.Archived() {
		return c, ErrArchived
	}
	name, question, applies, err := clean(in)
	if err != nil {
		return c, err
	}
	if in.Interval < 0 {
		return c, ErrIntervalNegative
	}
	next := c
	next.Name = name
	next.Question = question
	next.AppliesTo = applies
	next.Interval = in.Interval
	next.Enabled = in.Enabled
	next.Human = in.Human
	next.Version = c.Version + 1
	next.UpdatedAt = at
	return next, nil
}

func (c Check) Archive(at time.Time) (Check, error) {
	if at.IsZero() {
		return c, ErrTimeRequired
	}
	if c.Archived() {
		return c, ErrArchived
	}
	next := c
	next.Status = StatusArchived
	next.ArchivedAt = at
	next.UpdatedAt = at
	next.Version = c.Version + 1
	return next, nil
}

// clean is shared by New and Update so the two cannot drift about what an empty
// AppliesTo means. It sorts and de-duplicates, because the set is compared and
// rendered and a stored order nobody chose is a diff nobody made.
func clean(in Draft) (name, question string, applies []Subject, err error) {
	name = strings.TrimSpace(in.Name)
	if name == "" {
		return "", "", nil, ErrNameRequired
	}
	if len(name) > MaxNameLength {
		return "", "", nil, ErrNameTooLong
	}
	question = strings.TrimSpace(in.Question)
	if question == "" {
		return "", "", nil, ErrQuestionRequired
	}
	if len(question) > MaxQuestionLength {
		return "", "", nil, ErrQuestionTooLong
	}
	seen := map[Subject]bool{}
	for _, s := range in.AppliesTo {
		if seen[s] {
			continue
		}
		seen[s] = true
		applies = append(applies, s)
	}
	if len(applies) == 0 {
		return "", "", nil, ErrAppliesEmpty
	}
	sort.Slice(applies, func(i, j int) bool { return applies[i] < applies[j] })
	return name, question, applies, nil
}

const (
	EventCheckAdded    = "check.added"
	EventCheckUpdated  = "check.updated"
	EventCheckArchived = "check.archived"
	EventChainSaved    = "check.chain.saved"

	// The subject is the ORG — decisions/0024 reads it to make the entry
	// org-scoped, and 0031 says why a check has no workspace to be tenanted to.
	SubjectKind = "org"
)

type Added struct {
	CheckID   string   `json:"check_id"`
	OrgID     string   `json:"org_id"`
	Name      string   `json:"name"`
	AppliesTo []string `json:"applies_to"`
}

type Updated struct {
	CheckID string `json:"check_id"`
	OrgID   string `json:"org_id"`
	Name    string `json:"name"`
	// Both sets, because narrowing AppliesTo silently changes every coverage
	// denominator this check contributes to and nothing else records that.
	From []string `json:"applies_to_from"`
	To   []string `json:"applies_to_to"`
}

type Archived struct {
	CheckID string `json:"check_id"`
	OrgID   string `json:"org_id"`
	Name    string `json:"name"`
}

type ChainSaved struct {
	CheckID string `json:"check_id"`
	OrgID   string `json:"org_id"`
	Steps   int    `json:"steps"`
	Flows   int    `json:"flows"`
}

// Names renders a subject set for an envelope. Enums cross a module boundary as
// their SPELLING, never as an ordinal — inserting a value into the iota block
// later would otherwise reinterpret every stored and published set.
func Names(of []Subject) []string {
	out := make([]string, 0, len(of))
	for _, s := range of {
		out = append(out, s.String())
	}
	return out
}
