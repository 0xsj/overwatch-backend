package query

import (
	"context"
	"errors"

	"github.com/0xsj/overwatch-backend/internal/assistance/domain"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

type ProviderPolicyReader interface {
	ProviderPolicy(context.Context, id.ID) (domain.ProviderPolicy, error)
}

type ProviderPolicies struct{ reader ProviderPolicyReader }

func NewProviderPolicies(reader ProviderPolicyReader) *ProviderPolicies {
	if reader == nil {
		panic("assistance: NewProviderPolicies with a nil reader")
	}
	return &ProviderPolicies{reader: reader}
}

func (p *ProviderPolicies) Current(ctx context.Context, workspace id.ID) (domain.ProviderPolicy, error) {
	if workspace.IsZero() {
		return domain.ProviderPolicy{}, domain.ErrWorkspaceRequired
	}
	found, err := p.reader.ProviderPolicy(ctx, workspace)
	if errors.Is(err, domain.ErrNotFound) {
		return domain.DefaultProviderPolicy(workspace), nil
	}
	if err != nil {
		return domain.ProviderPolicy{}, err
	}
	return found, nil
}
