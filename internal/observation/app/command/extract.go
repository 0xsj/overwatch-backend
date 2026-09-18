package command

import (
	"context"
	"fmt"
	"time"

	"github.com/0xsj/overwatch-backend/internal/observation/domain"
	"github.com/0xsj/overwatch-backend/pkg/events"
	"github.com/0xsj/overwatch-backend/pkg/id"
	"github.com/0xsj/overwatch-backend/pkg/provenance"
)

type Repository interface {
	Create(ctx context.Context, o domain.Observation) error
	RecordUnmapped(ctx context.Context, u domain.Unmapped) error
}

// Mappings is the port into `tool`. It answers with observation's OWN types,
// because `tool` is a peer this package may not import.
//
// **`Live` is the whole contract**: an observation cites the version that was
// live when it ran, and asking for anything else would let a re-extraction
// silently cite a draft.
type Mappings interface {
	// Live answers the tool's live mapping versions and its PRODUCES-KIND.
	//
	// The second used to double as the subject FIELD NAME — decisions/0035 §2 —
	// and after `0040` it is only the kind: which mapping is the subject is
	// carried on the mapping itself, as a role. It stays because it is what the
	// subject value is a value OF, which no mapping knows.
	Live(ctx context.Context, org, tool id.ID) (mappings []domain.Mapping, subjectKind string, findings bool, err error)
}

// Findings is the port into `finding` — decisions/0041 §2. A tool that produces
// findings writes NO observations: what it says is about a problem, not about
// the subject, and filing `name: Log4j RCE` as an observation of the URL would
// put it in that asset's field list where it reads as a property of the url.
//
// `observation` may not import `finding` (peers), so the composition root adapts
// it — the same shape `run` reaches `observation` through.
type Findings interface {
	Record(ctx context.Context, in []Sighting) error
}

// Sighting is one finding-shaped record, in observation's own vocabulary.
type Sighting struct {
	WorkspaceID  id.ID
	OrgID        id.ID
	ToolID       id.ID
	InvocationID id.ID
	ArtifactID   id.ID

	Signature        string
	SignatureMapping id.ID
	Severity         string

	SubjectKind  string
	SubjectValue string

	Details []SightingDetail
	SeenAt  time.Time
}

type SightingDetail struct {
	Field   string
	Value   string
	Mapping id.ID
}

type Minter interface{ NewID() id.ID }

type Clock interface{ Now() time.Time }

// Extractor reads an artifact under a tool's live mappings.
type Extractor struct {
	repo      Repository
	mappings  Mappings
	findings  Findings
	publisher events.Publisher
	ids       Minter
	clock     Clock
}

func NewExtractor(repo Repository, mappings Mappings, findings Findings,
	publisher events.Publisher, ids Minter, clock Clock) *Extractor {
	if repo == nil || mappings == nil || findings == nil || publisher == nil ||
		ids == nil || clock == nil {
		panic("observation: NewExtractor with a nil dependency")
	}
	return &Extractor{repo: repo, mappings: mappings, findings: findings,
		publisher: publisher, ids: ids, clock: clock}
}

// Source is everything extraction needs about one artifact. It is a struct
// because six ids and a byte slice as positional arguments is a call site where
// two uuids get transposed and nothing complains.
type Source struct {
	WorkspaceID  id.ID
	OrgID        id.ID
	InvocationID id.ID
	ArtifactID   id.ID
	ToolID       id.ID

	Body []byte

	// MediaType is what the bytes were DECLARED to be, off the invocation's
	// argv — `root/runports.go` reads it there and never sniffs it. Extraction
	// did not take it until 2026-09-08 and assumed JSON, which made every
	// line-oriented tool in the corpus produce nothing at all. Empty is JSON,
	// which is what was assumed before the question was asked.
	MediaType string

	// ObservedAt is when the TOOL RAN — the invocation's start. Passing it in
	// rather than reading a clock is what stops a re-extraction making a
	// three-month-old reading look fresh.
	ObservedAt time.Time
}

// Result is what the caller records against the invocation, and what the
// Extraction Quality panel is built on.
type Result struct {
	Observations int
	FieldsSeen   int
	Mapped       int
	LeftAlone    int
	Records      int
}

// Extract reads the bytes and writes what they said.
//
// **A tool with no live mappings extracts NOTHING and that is not an error.**
// Nobody has taught this system to read that tool yet, which is a true and
// ordinary state — every tool is in it the moment it is added. The refusal is
// reserved for a tool that HAS mappings and none of them names its own
// produces-kind, because then somebody has begun and left it unusable.
func (e *Extractor) Extract(ctx context.Context, in Source) (Result, error) {
	mappings, subjectKind, findings, err := e.mappings.Live(ctx, in.OrgID, in.ToolID)
	if err != nil {
		return Result{}, err
	}
	if len(mappings) == 0 {
		return Result{}, nil
	}
	if subjectKind == "" {
		// The tool produces nothing this system can name a subject with. That
		// was `finding` until decisions/0041 gave one a subject of its own —
		// a finding-producing tool's subject kind is its CONSUMES — so what is
		// left here is a tool with no feeds at all.
		return Result{}, nil
	}

	extraction, err := domain.Extract(in.Body, mappings, subjectKind, domain.ShapeOf(in.MediaType))
	if err != nil {
		return Result{}, err
	}

	now := e.clock.Now()
	out := Result{
		Records:    len(extraction.Records),
		FieldsSeen: len(extraction.Seen),
		Mapped:     len(extraction.Mapped),
		LeftAlone:  len(extraction.LeftAlone),
	}

	// A FINDING-PRODUCING TOOL WRITES NO OBSERVATIONS — decisions/0041 §2. What
	// it said is about a problem, not about the subject: filing `name: Log4j
	// RCE` as an observation of the URL would put it in that asset's field list
	// where it reads as a property of the url.
	//
	// The field ACCOUNTING above still stands, which is the point of doing this
	// after the walk rather than instead of it: a nuclei template that grew a
	// field nobody mapped is still an unmapped path, and the Extraction Quality
	// panel still says so.
	if findings {
		if err := e.findings.Record(ctx, e.sightings(in, extraction)); err != nil {
			return Result{}, err
		}
		// THE UNMAPPED PATHS ARE STILL RECORDED. This branch returned before
		// writing them for one commit, so the event carried `left_alone: 1` and
		// the table it points at was empty — a count with no rows behind it,
		// which is worse than either alone because the panel looks right and
		// the drill-down is blank.
		if err := e.recordUnmapped(ctx, in, now, extraction); err != nil {
			return Result{}, err
		}
		return out, e.report(ctx, in, out, extraction)
	}

	for _, record := range extraction.Records {
		for _, reading := range record.Readings {
			o, err := domain.New(e.ids.NewID(), in.WorkspaceID, in.InvocationID,
				in.ArtifactID, reading.Mapping.ID, reading.Mapping.Version,
				record.SubjectKind, record.SubjectValue,
				domain.FieldOf(reading.Mapping), reading.Value,
				reading.Mapping.Role, in.ObservedAt, now)
			if err != nil {
				// A single unusable reading — an empty value, an over-long one —
				// is SKIPPED rather than failing the artifact. The others are
				// still what the source said, and losing them to protect a
				// length limit is the wrong trade.
				continue
			}
			if err := e.repo.Create(ctx, o); err != nil {
				return Result{}, err
			}
			out.Observations++
		}
	}

	if err := e.recordUnmapped(ctx, in, now, extraction); err != nil {
		return Result{}, err
	}
	return out, e.report(ctx, in, out, extraction)
}

// sightings turns a finding tool's records into what `finding` needs. A record
// with NO SIGNATURE is dropped here rather than downstream: half an identity is
// not a finding, and 0041 §1 makes the signature the half that makes a rescan a
// sighting rather than a new row.
func (e *Extractor) sightings(in Source, from domain.Extraction) []Sighting {
	out := make([]Sighting, 0, len(from.Records))
	for _, record := range from.Records {
		if record.Signature == "" {
			continue
		}
		one := Sighting{
			WorkspaceID: in.WorkspaceID, OrgID: in.OrgID, ToolID: in.ToolID,
			InvocationID: in.InvocationID, ArtifactID: in.ArtifactID,
			Signature: record.Signature, SignatureMapping: record.SignatureMapping,
			Severity:     record.Severity,
			SubjectKind:  record.SubjectKind,
			SubjectValue: record.SubjectValue,
			SeenAt:       in.ObservedAt,
		}
		for _, reading := range record.Readings {
			// Only the plain attributes become details. The subject, the
			// signature and the severity are already columns, and repeating
			// them would give a reader two places to look and one to trust.
			if reading.Mapping.Role != domain.RoleAttribute {
				continue
			}
			one.Details = append(one.Details, SightingDetail{
				Field: domain.FieldOf(reading.Mapping), Value: reading.Value,
				Mapping: reading.Mapping.ID,
			})
		}
		out = append(out, one)
	}
	return out
}

func (e *Extractor) recordUnmapped(ctx context.Context, in Source, now time.Time,
	extraction domain.Extraction) error {
	for _, left := range extraction.LeftAlone {
		u, err := domain.NewUnmapped(e.ids.NewID(), in.WorkspaceID, in.InvocationID,
			in.ArtifactID, left.Path, left.Sample, left.Seen, now)
		if err != nil {
			continue
		}
		if err := e.repo.RecordUnmapped(ctx, u); err != nil {
			return err
		}
	}
	return nil
}

// report emits the two events. It is shared by both paths, because the FIELD
// ACCOUNTING is the same question whether the tool produced observations or
// findings — "the tool grew two fields after an upgrade" is worth being told
// about either way.
func (e *Extractor) report(ctx context.Context, in Source, out Result,
	extraction domain.Extraction) error {
	paths := make([]string, 0, len(extraction.LeftAlone))
	for _, left := range extraction.LeftAlone {
		paths = append(paths, left.Path)
	}

	if err := e.emit(ctx, domain.EventObservationsCreated, in.WorkspaceID, domain.Created{
		WorkspaceID: in.WorkspaceID.String(), InvocationID: in.InvocationID.String(),
		ArtifactID: in.ArtifactID.String(), Records: out.Records,
		FieldsSeen: out.FieldsSeen, Mapped: out.Mapped, LeftAlone: out.LeftAlone,
	}); err != nil {
		return err
	}
	if len(paths) == 0 {
		return nil
	}
	// A SEPARATE event, because "the tool grew two fields after an upgrade" is
	// the thing somebody wants to be told about, and burying it inside a
	// success count is how it goes unnoticed for a quarter.
	return e.emit(ctx, domain.EventFieldUnmapped, in.WorkspaceID, domain.FieldUnmapped{
		WorkspaceID: in.WorkspaceID.String(), InvocationID: in.InvocationID.String(),
		ArtifactID: in.ArtifactID.String(), Paths: paths,
	})
}

// emit publishes WORK, not a decision — decisions/0014. Extraction has an
// outcome and nobody chose anything: a mapping read what it read.
func (e *Extractor) emit(ctx context.Context, name string, workspace id.ID, payload any) error {
	prov, ok := provenance.Current(ctx)
	if !ok {
		prov = provenance.New(provenance.OriginRequest, e.ids)
	}
	tenanted, err := prov.WithTenant(workspace.String())
	if err != nil {
		return fmt.Errorf("observation: %s: %w", name, err)
	}
	ev, err := events.New(e.ids, e.clock, name,
		domain.SubjectKind+":"+workspace.String(), tenanted, payload)
	if err != nil {
		return fmt.Errorf("observation: %s: %w", name, err)
	}
	if err := e.publisher.Publish(ctx, ev); err != nil {
		return fmt.Errorf("observation: %s: %w", name, err)
	}
	return nil
}
