package root

import (
	"context"

	obscmd "github.com/0xsj/overwatch-backend/internal/observation/app/command"
	obsquery "github.com/0xsj/overwatch-backend/internal/observation/app/query"
	obsdomain "github.com/0xsj/overwatch-backend/internal/observation/domain"
	runcmd "github.com/0xsj/overwatch-backend/internal/run/app/command"
	runquery "github.com/0xsj/overwatch-backend/internal/run/app/query"
	scopequery "github.com/0xsj/overwatch-backend/internal/scope/app/query"
	toolquery "github.com/0xsj/overwatch-backend/internal/tool/app/query"
	tooldomain "github.com/0xsj/overwatch-backend/internal/tool/domain"
	"github.com/0xsj/overwatch-backend/pkg/errors"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

// This file is where `observation` meets the four domains it may not import,
// and where `run` reaches `observation`. All peers; the composition root is the
// only place any of them may know about the others.

// liveMappings is observation's port into `tool`. It answers with observation's
// OWN types, and it resolves the SUBJECT FIELD — the tool's produces-kind,
// spelled — which is decisions/0035 §2's rule made concrete.
type liveMappings struct{ tools *toolquery.Tools }

func (l liveMappings) Live(ctx context.Context, org, tool id.ID) ([]obsdomain.Mapping, string, error) {
	found, err := l.tools.ByID(ctx, org, tool)
	if err != nil {
		if errors.IsKind(err, errors.NotFound) {
			// A tool archived since the run was planned reads nothing rather
			// than failing the artifact — the bytes are already stored and
			// citable, which is the part that had to survive.
			return nil, "", nil
		}
		return nil, "", err
	}
	// A tool producing `finding` has no fragment kind to be a subject, so its
	// output is somebody else's noun — 0034 keeps `finding` out of the kind
	// vocabulary deliberately.
	subject := found.Produces.String()
	if found.Produces == tooldomain.FeedNone || found.Produces == tooldomain.FeedFinding {
		return nil, "", nil
	}

	versions, err := l.tools.Mappings(ctx, org, tool)
	if err != nil {
		return nil, "", err
	}
	out := make([]obsdomain.Mapping, 0, len(versions))
	for _, v := range versions {
		if !v.Live() {
			// ONLY LIVE VERSIONS. An observation cites the version that was
			// live when it ran; reading a draft would let an unpromoted
			// correction produce records nobody promoted.
			continue
		}
		path, err := obsdomain.ParsePath(v.Expression)
		if err != nil {
			// A mapping whose expression is not a path at all is skipped, not
			// fatal: the other fields of this tool still read, and the broken
			// one shows up as a field that never appears.
			continue
		}
		out = append(out, obsdomain.Mapping{
			ID: v.ID, Field: v.Field, Version: v.Version, Path: path,
		})
	}
	return out, subject, nil
}

// mappingStep is the PARSER VERSION half of a lineage.
type mappingStep struct{ tools *toolquery.Tools }

func (m mappingStep) Step(ctx context.Context, org, mapping id.ID) (*obsquery.MappingStep, error) {
	found, err := m.tools.Mapping(ctx, org, mapping)
	if err != nil {
		if errors.IsKind(err, errors.NotFound) {
			// nil, not an error: a missing step is a fact about the lineage.
			return nil, nil
		}
		return nil, err
	}
	return &obsquery.MappingStep{
		MappingID: found.ID, Field: found.Field, Expression: found.Expression,
		Version: found.Version, State: found.State.String(),
	}, nil
}

// runSteps is the RAW BYTES and EXACT COMMAND halves, plus the rule id the
// invocation cites — which is how the fourth step is reached without
// `observation` learning what a scope rule is.
type runSteps struct{ runs *runquery.Runs }

func (r runSteps) Steps(ctx context.Context, workspace, invocation, artifact id.ID) (
	*obsquery.InvocationStep, *obsquery.ArtifactStep, id.ID, error) {
	found, err := r.runs.Invocation(ctx, workspace, invocation)
	if err != nil {
		if errors.IsKind(err, errors.NotFound) {
			return nil, nil, id.ID{}, nil
		}
		return nil, nil, id.ID{}, err
	}
	step := &obsquery.InvocationStep{
		InvocationID: found.ID, RunID: found.RunID, ToolID: found.ToolID,
		Argv: found.Argv, Binary: found.Binary, Phase: found.Phase.String(),
		StartedAt: found.StartedAt,
	}
	if found.HasExitCode {
		code := found.ExitCode
		step.ExitCode = &code
	}

	var bytes *obsquery.ArtifactStep
	if a, err := r.runs.Artifact(ctx, workspace, artifact); err == nil {
		bytes = &obsquery.ArtifactStep{
			ArtifactID: a.ID, Stream: a.Stream.String(), Hash: a.Hash,
			Bytes: a.Bytes, Truncated: a.Truncated,
		}
	} else if !errors.IsKind(err, errors.NotFound) {
		return nil, nil, id.ID{}, err
	}
	// The rule that DECIDED this spawn: the one that permitted it, or the one
	// that refused it. Only one can be set — the domain and the schema both
	// refuse a row that carries both — so this is a choice between two nulls
	// rather than a precedence.
	rule := found.PermitRule
	if rule.IsZero() {
		rule = found.RefusalRule
	}
	return step, bytes, rule, nil
}

// ruleStep is WHY THE COMMAND WAS ALLOWED TO RUN. It reads the rule even when
// SUPERSEDED, which is precisely why decisions/0030 keeps it: three surfaces
// cite a rule id and each captured it at the time.
type ruleStep struct{ rules *scopequery.Rules }

func (s ruleStep) Step(ctx context.Context, workspace, rule id.ID) (*obsquery.RuleStep, error) {
	found, err := s.rules.ByID(ctx, workspace, rule)
	if err != nil {
		if errors.IsKind(err, errors.NotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &obsquery.RuleStep{
		RuleID: found.ID, Pattern: found.Pattern, Polarity: found.Polarity.String(),
		Gate: found.Gate.String(), Superseded: found.Superseded(),
	}, nil
}

// extracts is `run`'s port into `observation` — the only edge that runs in that
// direction.
type extracts struct{ extractor *obscmd.Extractor }

func (e extracts) Extract(ctx context.Context, in runcmd.Extraction) (int, error) {
	got, err := e.extractor.Extract(ctx, obscmd.Source{
		WorkspaceID: in.WorkspaceID, OrgID: in.OrgID, InvocationID: in.InvocationID,
		ArtifactID: in.ArtifactID, ToolID: in.ToolID, Body: in.Body,
		ObservedAt: in.ObservedAt,
	})
	if err != nil {
		return 0, err
	}
	return got.Observations, nil
}
