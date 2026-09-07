package domain

import (
	"strings"
	"time"

	"github.com/0xsj/overwatch-backend/pkg/id"
)

const MaxPatternLength = 400

// Gate is which question a rule answers — decisions/0010.
type Gate uint8

const (
	GateSpawn Gate = iota
	GateClaim
)

func (g Gate) String() string {
	if g == GateClaim {
		return "claim"
	}
	return "spawn"
}

func ParseGate(s string) (Gate, error) {
	switch s {
	case "spawn":
		return GateSpawn, nil
	case "claim":
		return GateClaim, nil
	default:
		return GateSpawn, ErrGateUnknown
	}
}

type Polarity uint8

const (
	PolarityInclude Polarity = iota
	PolarityExclude
)

func (p Polarity) String() string {
	if p == PolarityExclude {
		return "exclude"
	}
	return "include"
}

func ParsePolarity(s string) (Polarity, error) {
	switch s {
	case "include":
		return PolarityInclude, nil
	case "exclude":
		return PolarityExclude, nil
	default:
		return PolarityInclude, ErrPolarityUnknown
	}
}

// Kind is what a rule can match, and it is THE KIND VOCABULARY — decisions/0034.
// Fourteen values: `0009`'s thirteen fragment kinds plus `url`.
//
// **`scope` holds the canonical copy because a rule may name any of them.**
// `tool` and `check` hold their own copies — U1 forbids the import, they are
// peers — and `root` holds a test that fails when the three drift, because the
// composition root is the one place allowed to see all three.
//
// There is deliberately NO `domain`. A domain is a HOST IN A ROLE, which is
// `0009`'s own argument about assets applied one level down: `acme.com` and
// `www.acme.com` are both hosts, and "the one we started from" is a role. A
// second kind would force every consumer of a host pattern to decide whether it
// also matches a domain.
//
// The two gates take DISJOINT sets — 0010 — and `Gate()` is what enforces it,
// so a kind cannot be silently accepted on the wrong one.
type Kind uint8

const (
	KindHost Kind = iota
	KindCIDR
	KindIP
	KindASN
	KindURL
	KindRepo
	KindEmail
	KindAccount
	KindDocument
	KindOrg
	KindPerson
	KindCert
	KindKey
	KindWhois
)

var kindNames = map[Kind]string{
	KindHost: "host", KindCIDR: "cidr", KindIP: "ip",
	KindASN: "asn", KindURL: "url",
	KindRepo: "repo", KindEmail: "email", KindAccount: "account",
	KindDocument: "document", KindOrg: "org", KindPerson: "person",
	KindCert: "cert", KindKey: "key", KindWhois: "whois",
}

// Every is the vocabulary, in a stable order. It is a function rather than a
// package variable so a caller cannot append to the shared slice — the same
// reason check.EverySubject is one.
func Every() []Kind {
	return []Kind{
		KindHost, KindCIDR, KindIP, KindASN, KindURL,
		KindRepo, KindEmail, KindAccount, KindDocument, KindOrg, KindPerson,
		KindCert, KindKey, KindWhois,
	}
}

// Targetable says whether a fragment of this kind can be an ASSET — 0009: "a
// fragment whose kind is targetable and which carries an accepted attribution
// to the target's root entity".
//
// It is a FACET of this list rather than a second list, which is the whole of
// decisions/0034. `check` declares its coverage subjects as exactly this set,
// and the root test is what says so.
//
// **Spawnable is a subset of targetable** — you cannot spawn against something
// that is not an asset — and that containment is an invariant rather than a
// coincidence. The root test asserts it.
func (k Kind) Targetable() bool {
	switch k {
	case KindHost, KindCIDR, KindIP, KindASN, KindURL, KindRepo, KindEmail:
		return true
	default:
		return false
	}
}

func (k Kind) String() string {
	if s, ok := kindNames[k]; ok {
		return s
	}
	return "host"
}

func ParseKind(s string) (Kind, error) {
	for k, name := range kindNames {
		if name == s {
			return k, nil
		}
	}
	return KindHost, ErrKindUnknown
}

// Gate reports which gate this kind belongs to. Most kinds can never match a
// spawn rule at all, because nothing is ever spawned against a repository —
// 0010.
//
// `asn` and `url` were added on 2026-09-07 (decisions/0034): `asnmap` and
// `whois` take an ASN, `nuclei` and `httpx` take a URL, and the previous set
// meant an ASN could never be in scope on either gate — which contradicted
// `0009`, where an ASN is targetable, and made `0011` unimplementable, since its
// entire worked example is an ASN with no TLS certificate.
func (k Kind) Gate() Gate {
	switch k {
	case KindHost, KindCIDR, KindIP, KindASN, KindURL:
		return GateSpawn
	default:
		return GateClaim
	}
}

// Intensity qualifies a SPAWN rule: "a range in scope for passive collection is
// not thereby in scope for a loud scan". It is absent on a claim rule, because
// there are no processes on that gate — absent rather than empty or wildcard.
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

// Rule is one line of a target's scope.
//
// **It is never edited** — decisions/0030. `SupersededAt` is the only field that
// changes after it is written, because three surfaces cite a rule by id and each
// captures it at the time.
type Rule struct {
	ID          id.ID
	WorkspaceID id.ID
	TargetID    id.ID

	Pattern  string
	Polarity Polarity
	Gate     Gate
	Kinds    []Kind

	// Tools is ABSENT on a claim rule and non-empty on a spawn rule.
	Tools []Intensity

	CreatedBy    id.ID
	CreatedAt    time.Time
	SupersededAt time.Time
	SupersededBy id.ID
}

func NewRule(newID, workspace, target, createdBy id.ID, pattern string,
	polarity Polarity, gate Gate, kinds []Kind, tools []Intensity, at time.Time) (Rule, error) {
	if newID.IsZero() || createdBy.IsZero() {
		return Rule{}, ErrIDRequired
	}
	if workspace.IsZero() {
		return Rule{}, ErrWorkspaceRequired
	}
	if target.IsZero() {
		return Rule{}, ErrTargetRequired
	}
	if at.IsZero() {
		return Rule{}, ErrTimeRequired
	}
	pattern = strings.ToLower(strings.TrimSpace(pattern))
	if pattern == "" {
		return Rule{}, ErrPatternRequired
	}
	if len(pattern) > MaxPatternLength {
		return Rule{}, ErrPatternTooLong
	}
	if len(kinds) == 0 {
		// A rule that names no kinds matches nothing, which is a rule somebody
		// wrote and will believe is working.
		return Rule{}, ErrKindsRequired
	}
	for _, k := range kinds {
		if k.Gate() != gate {
			return Rule{}, ErrKindWrongGate
		}
	}
	if gate == GateClaim && len(tools) > 0 {
		return Rule{}, ErrToolsOnClaim
	}
	if gate == GateSpawn && len(tools) == 0 {
		// Every intensity, spelled out. An empty list on a spawn rule reads as
		// "no intensities" to anything that filters by them, and as "all" to
		// anything that does not — which is the absent/empty collapse.
		tools = []Intensity{IntensityPassive, IntensityLight, IntensityLoud}
	}
	return Rule{
		ID: newID, WorkspaceID: workspace, TargetID: target,
		Pattern: pattern, Polarity: polarity, Gate: gate,
		Kinds: kinds, Tools: tools,
		CreatedBy: createdBy, CreatedAt: at,
	}, nil
}

func (r Rule) Superseded() bool { return !r.SupersededAt.IsZero() }

// Supersede retires a rule and KEEPS ITS ID. There is no edit — decisions/0030.
func (r Rule) Supersede(by id.ID, at time.Time) (Rule, error) {
	if at.IsZero() {
		return r, ErrTimeRequired
	}
	if r.Superseded() {
		return r, ErrSuperseded
	}
	next := r
	next.SupersededAt = at
	next.SupersededBy = by
	return next, nil
}

func (r Rule) matchesKind(k Kind) bool {
	for _, held := range r.Kinds {
		if held == k {
			return true
		}
	}
	return false
}

func (r Rule) allowsIntensity(i Intensity) bool {
	if r.Gate == GateClaim {
		return true
	}
	for _, held := range r.Tools {
		if held == i {
			return true
		}
	}
	return false
}

const (
	EventRuleAdded      = "scope.rule.added"
	EventRuleSuperseded = "scope.rule.superseded"

	// The subject is the TARGET, not the rule: a scope edit is a thing that
	// happened to a target, and that is the row somebody reads the history of.
	SubjectKind = "target"
)

type RuleAdded struct {
	RuleID      string `json:"rule_id"`
	TargetID    string `json:"target_id"`
	WorkspaceID string `json:"workspace_id"`
	Pattern     string `json:"pattern"`
	Polarity    string `json:"polarity"`
	Gate        string `json:"gate"`
}

type RuleSuperseded struct {
	RuleID      string `json:"rule_id"`
	TargetID    string `json:"target_id"`
	WorkspaceID string `json:"workspace_id"`
	Pattern     string `json:"pattern"`
}
