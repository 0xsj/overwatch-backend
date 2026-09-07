package query

import (
	"context"
	"fmt"

	"github.com/0xsj/overwatch-backend/internal/tool/domain"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

type Reader interface {
	ByID(ctx context.Context, org, want id.ID) (domain.Tool, error)
	ForOrg(ctx context.Context, org id.ID, includeArchived bool) ([]domain.Tool, error)
	MappingByID(ctx context.Context, org, want id.ID) (domain.Mapping, error)
	MappingsForTool(ctx context.Context, org, tool id.ID) ([]domain.Mapping, error)
	LiveMapping(ctx context.Context, tool id.ID, field string) (domain.Mapping, error)
}

type Tools struct{ reader Reader }

func NewTools(reader Reader) *Tools {
	if reader == nil {
		panic("tool: NewTools query with a nil reader")
	}
	return &Tools{reader: reader}
}

func (t *Tools) ByID(ctx context.Context, org, want id.ID) (domain.Tool, error) {
	if org.IsZero() || want.IsZero() {
		return domain.Tool{}, domain.ErrIDRequired
	}
	return t.reader.ByID(ctx, org, want)
}

func (t *Tools) ForOrg(ctx context.Context, org id.ID, includeArchived bool) ([]domain.Tool, error) {
	if org.IsZero() {
		return nil, domain.ErrOrgRequired
	}
	found, err := t.reader.ForOrg(ctx, org, includeArchived)
	if err != nil {
		return nil, fmt.Errorf("tool: tools: %w", err)
	}
	return found, nil
}

// Mappings answers with every version, retired ones included, newest first
// within each field. They are the history the citations point at — an
// observation names the version that produced it, and dropping retired rows from
// this read makes that citation dangle.
func (t *Tools) Mappings(ctx context.Context, org, tool id.ID) ([]domain.Mapping, error) {
	if org.IsZero() || tool.IsZero() {
		return nil, domain.ErrIDRequired
	}
	found, err := t.reader.MappingsForTool(ctx, org, tool)
	if err != nil {
		return nil, fmt.Errorf("tool: mappings: %w", err)
	}
	return found, nil
}

// Live is the runner's read, and it is the one a pipeline will call: what turns
// this tool's output into this field right now. A field with no live version
// answers ErrMappingGone rather than an empty mapping — "no rule says how" and
// "the rule says nothing" are not the same claim.
func (t *Tools) Live(ctx context.Context, tool id.ID, field string) (domain.Mapping, error) {
	if tool.IsZero() {
		return domain.Mapping{}, domain.ErrIDRequired
	}
	if field == "" {
		return domain.Mapping{}, domain.ErrFieldRequired
	}
	return t.reader.LiveMapping(ctx, tool, field)
}

// Mapping is one version by id, and it exists for LINEAGE: an observation cites
// a mapping version, and walking back to "what read this value, and how" is
// PRODUCT.md's first step. Reading the whole tool's history to find one row
// would be a list read serving a point lookup.
func (t *Tools) Mapping(ctx context.Context, org, want id.ID) (domain.Mapping, error) {
	if org.IsZero() || want.IsZero() {
		return domain.Mapping{}, domain.ErrIDRequired
	}
	return t.reader.MappingByID(ctx, org, want)
}
