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
	// Live answers the tool's live mapping versions, and the field name that
	// carries the subject — which is the tool's produces-kind, spelled.
	Live(ctx context.Context, org, tool id.ID) (mappings []domain.Mapping, subjectKind string, err error)
}

type Minter interface{ NewID() id.ID }

type Clock interface{ Now() time.Time }

// Extractor reads an artifact under a tool's live mappings.
type Extractor struct {
	repo      Repository
	mappings  Mappings
	publisher events.Publisher
	ids       Minter
	clock     Clock
}

func NewExtractor(repo Repository, mappings Mappings, publisher events.Publisher,
	ids Minter, clock Clock) *Extractor {
	if repo == nil || mappings == nil || publisher == nil || ids == nil || clock == nil {
		panic("observation: NewExtractor with a nil dependency")
	}
	return &Extractor{repo: repo, mappings: mappings, publisher: publisher, ids: ids, clock: clock}
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
	mappings, subjectKind, err := e.mappings.Live(ctx, in.OrgID, in.ToolID)
	if err != nil {
		return Result{}, err
	}
	if len(mappings) == 0 {
		return Result{}, nil
	}
	if subjectKind == "" {
		// The tool produces nothing nameable — `finding` today. Its output is
		// somebody else's noun.
		return Result{}, nil
	}

	extraction, err := domain.Extract(in.Body, mappings, subjectKind, subjectKind)
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

	for _, record := range extraction.Records {
		for _, reading := range record.Readings {
			o, err := domain.New(e.ids.NewID(), in.WorkspaceID, in.InvocationID,
				in.ArtifactID, reading.Mapping.ID, reading.Mapping.Version,
				record.SubjectKind, record.SubjectValue,
				domain.FieldOf(reading.Mapping), reading.Value,
				in.ObservedAt, now)
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

	paths := make([]string, 0, len(extraction.LeftAlone))
	for _, left := range extraction.LeftAlone {
		u, err := domain.NewUnmapped(e.ids.NewID(), in.WorkspaceID, in.InvocationID,
			in.ArtifactID, left.Path, left.Sample, left.Seen, now)
		if err != nil {
			continue
		}
		if err := e.repo.RecordUnmapped(ctx, u); err != nil {
			return Result{}, err
		}
		paths = append(paths, left.Path)
	}

	if err := e.emit(ctx, domain.EventObservationsCreated, in.WorkspaceID, domain.Created{
		WorkspaceID: in.WorkspaceID.String(), InvocationID: in.InvocationID.String(),
		ArtifactID: in.ArtifactID.String(), Records: out.Records,
		FieldsSeen: out.FieldsSeen, Mapped: out.Mapped, LeftAlone: out.LeftAlone,
	}); err != nil {
		return Result{}, err
	}
	if len(paths) == 0 {
		return out, nil
	}
	// A SEPARATE event, because "the tool grew two fields after an upgrade" is
	// the thing somebody wants to be told about, and burying it inside a
	// success count is how it goes unnoticed for a quarter.
	return out, e.emit(ctx, domain.EventFieldUnmapped, in.WorkspaceID, domain.FieldUnmapped{
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
