package query

import (
	"context"
	"sort"
	"time"

	"github.com/0xsj/overwatch-backend/internal/health/domain"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

// Probes is the six reads, one per kind of silence. They are one interface
// because they are asked together and never apart, and because six fields on a
// struct that are always set together is six chances to set five.
//
// **Every method answers `health`'s own types.** This package imports no peer;
// the composition root adapts each, which is also where the vocabulary
// translation lives.
//
// A method that returns an error does NOT fail the report — see [Doctor.Examine].
type Probes interface {
	// ToolsThatCouldNotStart reads `run.invocation.unavailable`, which `execx`
	// writes when a tool is off PATH or not executable.
	ToolsThatCouldNotStart(ctx context.Context, workspace id.ID) ([]domain.Symptom, int, error)

	// ToolsNobodyReads is a tool with no live mappings: it spawns, its bytes are
	// stored, and nothing is read out of them.
	ToolsNobodyReads(ctx context.Context, workspace id.ID) ([]domain.Symptom, int, error)

	// FindingToolsWithNoSignature — decisions/0041 §2's quiet failure.
	FindingToolsWithNoSignature(ctx context.Context, workspace id.ID) ([]domain.Symptom, int, error)

	// ChecksThatCannotRun is enabled, on a clock, and chainless.
	ChecksThatCannotRun(ctx context.Context, workspace id.ID) ([]domain.Symptom, int, error)

	// EventsThatGaveUp is owed item B: `pkg/outbox` says a buried row is the
	// alarm, and nothing read them.
	EventsThatGaveUp(ctx context.Context, workspace id.ID) ([]domain.Symptom, int, error)

	// FieldsNobodyMapped is a tool saying something nobody taught this system
	// to read.
	FieldsNobodyMapped(ctx context.Context, workspace id.ID) ([]domain.Symptom, int, error)
}

type Clock interface{ Now() time.Time }

// Doctor answers *"is the machinery well"* — `CLAUDE.md`'s `health`, which is
// DERIVED: it reads and never writes, and this package has no store at all.
type Doctor struct {
	probes Probes
	clock  Clock
}

func NewDoctor(probes Probes, clock Clock) *Doctor {
	if probes == nil || clock == nil {
		panic("health: NewDoctor with a nil dependency")
	}
	return &Doctor{probes: probes, clock: clock}
}

// Examine runs every probe and reports what each one found — **including the
// ones that found nothing, and especially the ones that could not run.**
//
// **A FAILING PROBE DOES NOT FAIL THE REPORT.** If reading the outbox errors,
// the honest answer is five results and one `–`, not a 500: a health screen that
// goes blank when one of its six reads breaks has become the thing it exists to
// detect. The failure is recorded on that probe and [domain.Report.Trustworthy]
// turns false, so "nothing is wrong" stops being sayable.
func (d *Doctor) Examine(ctx context.Context, workspace id.ID) (domain.Report, error) {
	if workspace.IsZero() {
		return domain.Report{}, domain.ErrWorkspaceRequired
	}
	out := domain.Report{
		At:       d.clock.Now(),
		Probes:   make([]domain.Probe, 0, len(domain.All)),
		Symptoms: []domain.Symptom{},
	}

	for _, kind := range domain.All {
		found, looked, err := d.run(ctx, workspace, kind)
		probe := domain.Probe{Kind: kind, Measured: err == nil}
		if err != nil {
			probe.Because = err.Error()
			out.Probes = append(out.Probes, probe)
			continue
		}
		probe.Looked = looked
		probe.Found = len(found)
		out.Probes = append(out.Probes, probe)
		out.Symptoms = append(out.Symptoms, found...)
	}

	// WORST KIND FIRST, then by how many. `domain.All` is the severity order and
	// it is used here rather than a second list, so the two cannot disagree.
	rank := make(map[domain.Kind]int, len(domain.All))
	for n, kind := range domain.All {
		rank[kind] = n
	}
	sort.SliceStable(out.Symptoms, func(i, j int) bool {
		a, b := out.Symptoms[i], out.Symptoms[j]
		if rank[a.Kind] != rank[b.Kind] {
			return rank[a.Kind] < rank[b.Kind]
		}
		if a.Count != b.Count {
			return a.Count > b.Count
		}
		return a.Subject < b.Subject
	})
	return out, nil
}

func (d *Doctor) run(ctx context.Context, workspace id.ID, kind domain.Kind) ([]domain.Symptom, int, error) {
	switch kind {
	case domain.ToolUnavailable:
		return d.probes.ToolsThatCouldNotStart(ctx, workspace)
	case domain.ToolNoSignature:
		return d.probes.FindingToolsWithNoSignature(ctx, workspace)
	case domain.EventBuried:
		return d.probes.EventsThatGaveUp(ctx, workspace)
	case domain.CheckUnrunnable:
		return d.probes.ChecksThatCannotRun(ctx, workspace)
	case domain.ToolUnread:
		return d.probes.ToolsNobodyReads(ctx, workspace)
	case domain.FieldUnmapped:
		return d.probes.FieldsNobodyMapped(ctx, workspace)
	}
	// Unreachable while `All` and this switch agree, and a report that silently
	// skipped a kind would be exactly the untrustworthy-but-clean answer this
	// package exists to refuse. So it is an ERROR on that probe rather than a
	// missing entry.
	return nil, 0, domain.ErrProbeUnknown
}
