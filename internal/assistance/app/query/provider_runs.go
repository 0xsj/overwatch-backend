package query

import (
	"context"

	"github.com/0xsj/overwatch-backend/internal/assistance/domain"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

type ProviderRunReader interface {
	PageProviderRuns(context.Context, id.ID, int) ([]domain.ProviderRun, error)
}

type ProviderRuns struct{ reader ProviderRunReader }

func NewProviderRuns(reader ProviderRunReader) *ProviderRuns {
	if reader == nil {
		panic("assistance: NewProviderRuns with a nil reader")
	}
	return &ProviderRuns{reader: reader}
}

type ProviderRunPage struct {
	Items []domain.ProviderRun `json:"items"`
}

func (p *ProviderRuns) List(ctx context.Context, workspace id.ID, limit int) (ProviderRunPage, error) {
	if workspace.IsZero() {
		return ProviderRunPage{}, domain.ErrIDRequired
	}
	if limit <= 0 {
		limit = 50
	}
	if limit > 100 {
		limit = 100
	}
	items, err := p.reader.PageProviderRuns(ctx, workspace, limit)
	if err != nil {
		return ProviderRunPage{}, err
	}
	if items == nil {
		items = []domain.ProviderRun{}
	}
	return ProviderRunPage{Items: items}, nil
}
