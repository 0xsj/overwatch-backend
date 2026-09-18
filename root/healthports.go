package root

import (
	"context"
	"fmt"

	checkpg "github.com/0xsj/overwatch-backend/internal/check/infra/postgres"
	healthdomain "github.com/0xsj/overwatch-backend/internal/health/domain"
	obspg "github.com/0xsj/overwatch-backend/internal/observation/infra/postgres"
	runpg "github.com/0xsj/overwatch-backend/internal/run/infra/postgres"
	toolpg "github.com/0xsj/overwatch-backend/internal/tool/infra/postgres"
	workspacequery "github.com/0xsj/overwatch-backend/internal/workspace/app/query"
	"github.com/0xsj/overwatch-backend/pkg/id"
	"github.com/0xsj/overwatch-backend/pkg/outbox"
)

// This file is where `health` meets the six places machinery goes quiet.
//
// **Each probe answers a COUNT as well as a list**, and that is the whole
// design: a clean result means nothing without its denominator. "No tool failed
// to start" is a measurement only beside "out of 412 invocations"; on its own it
// is indistinguishable from a system that has never run anything.
type probes struct {
	runs       *runpg.Store
	tools      *toolpg.Store
	checks     *checkpg.Store
	observed   *obspg.Store
	events     *outbox.Postgres
	workspaces *workspacequery.Workspaces
}

// org resolves the workspace's org, because `tool` and `check` are ORG-SCOPED
// (0031) and health is asked per engagement. It is the same join `run` cannot
// avoid.
func (p probes) org(ctx context.Context, workspace id.ID) (id.ID, error) {
	org, _, err := p.workspaces.OrgOf(ctx, workspace)
	return org, err
}

func (p probes) ToolsThatCouldNotStart(ctx context.Context, workspace id.ID) ([]healthdomain.Symptom, int, error) {
	found, err := p.runs.Unavailable(ctx, workspace)
	if err != nil {
		return nil, 0, err
	}
	looked, err := p.runs.CountInvocations(ctx, workspace)
	if err != nil {
		return nil, 0, err
	}
	out := make([]healthdomain.Symptom, 0, len(found))
	for _, one := range found {
		out = append(out, healthdomain.Symptom{
			Kind: healthdomain.ToolUnavailable,
			// The TOOL's id, not the invocation's: a person acts on the tool.
			SubjectID: one.ToolID, Subject: p.toolName(ctx, workspace, one.ToolID),
			Detail: one.Reason, Count: one.Seen, Since: one.Since,
		})
	}
	return out, looked, nil
}

// toolName turns an id into the word a person uses. A symptom naming only a
// uuid is a symptom nobody can act on — and a lookup that FAILS falls back to
// the id rather than dropping the symptom, because a badly-labelled alarm still
// beats a missing one.
func (p probes) toolName(ctx context.Context, workspace, tool id.ID) string {
	org, err := p.org(ctx, workspace)
	if err != nil {
		return tool.String()
	}
	found, err := p.tools.ByID(ctx, org, tool)
	if err != nil {
		return tool.String()
	}
	return found.Name
}

func (p probes) ToolsNobodyReads(ctx context.Context, workspace id.ID) ([]healthdomain.Symptom, int, error) {
	org, err := p.org(ctx, workspace)
	if err != nil {
		return nil, 0, err
	}
	found, err := p.tools.Unread(ctx, org)
	if err != nil {
		return nil, 0, err
	}
	looked, err := p.tools.CountTools(ctx, org)
	if err != nil {
		return nil, 0, err
	}
	out := make([]healthdomain.Symptom, 0, len(found))
	for _, one := range found {
		out = append(out, healthdomain.Symptom{
			Kind: healthdomain.ToolUnread, SubjectID: one.ID, Subject: one.Name,
			Detail: "no live mapping reads its output", Count: 1, Since: one.Since,
		})
	}
	return out, looked, nil
}

func (p probes) FindingToolsWithNoSignature(ctx context.Context, workspace id.ID) ([]healthdomain.Symptom, int, error) {
	org, err := p.org(ctx, workspace)
	if err != nil {
		return nil, 0, err
	}
	found, err := p.tools.Unsigned(ctx, org)
	if err != nil {
		return nil, 0, err
	}
	looked, err := p.tools.CountTools(ctx, org)
	if err != nil {
		return nil, 0, err
	}
	out := make([]healthdomain.Symptom, 0, len(found))
	for _, one := range found {
		out = append(out, healthdomain.Symptom{
			Kind: healthdomain.ToolNoSignature, SubjectID: one.ID, Subject: one.Name,
			Detail: "produces findings and declares no signature mapping",
			Count:  1, Since: one.Since,
		})
	}
	return out, looked, nil
}

func (p probes) ChecksThatCannotRun(ctx context.Context, workspace id.ID) ([]healthdomain.Symptom, int, error) {
	org, err := p.org(ctx, workspace)
	if err != nil {
		return nil, 0, err
	}
	found, err := p.checks.Unrunnable(ctx, org)
	if err != nil {
		return nil, 0, err
	}
	looked, err := p.checks.CountChecks(ctx, org)
	if err != nil {
		return nil, 0, err
	}
	out := make([]healthdomain.Symptom, 0, len(found))
	for _, one := range found {
		out = append(out, healthdomain.Symptom{
			Kind: healthdomain.CheckUnrunnable, SubjectID: one.ID, Subject: one.Name,
			Detail: fmt.Sprintf("enabled every %ds and has no chain", one.IntervalSeconds),
			Count:  1,
		})
	}
	return out, looked, nil
}

// EventsThatGaveUp is owed item B. `pkg/outbox` justified not closing a race by
// saying *"a buried outbox row is the alarm"*, and the alarm was one ERROR line
// in a log nobody queries.
func (p probes) EventsThatGaveUp(ctx context.Context, workspace id.ID) ([]healthdomain.Symptom, int, error) {
	tenant := workspace.String()
	found, err := p.events.Buried(ctx, tenant)
	if err != nil {
		return nil, 0, err
	}
	looked, err := p.events.BuriedDepth(ctx, tenant)
	if err != nil {
		return nil, 0, err
	}
	out := make([]healthdomain.Symptom, 0, len(found))
	for _, one := range found {
		out = append(out, healthdomain.Symptom{
			Kind: healthdomain.EventBuried, Subject: one.Name,
			Detail: one.LastError, Count: one.Count, Since: one.Since,
		})
	}
	return out, looked, nil
}

func (p probes) FieldsNobodyMapped(ctx context.Context, workspace id.ID) ([]healthdomain.Symptom, int, error) {
	found, err := p.observed.UnmappedPaths(ctx, workspace, 200)
	if err != nil {
		return nil, 0, err
	}
	looked, err := p.observed.CountUnmapped(ctx, workspace)
	if err != nil {
		return nil, 0, err
	}
	out := make([]healthdomain.Symptom, 0, len(found))
	for _, one := range found {
		out = append(out, healthdomain.Symptom{
			Kind: healthdomain.FieldUnmapped, Subject: one.Path,
			Detail: fmt.Sprintf("seen %d times across %d invocations", one.Seen, one.Invocations),
			Count:  one.Seen, Since: one.Since,
		})
	}
	return out, looked, nil
}
