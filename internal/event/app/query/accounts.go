package query

import (
	"context"

	"github.com/0xsj/overwatch-backend/internal/event/domain"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

type AccountReader interface {
	Accounts(context.Context, id.ID, id.ID) ([]domain.Account, error)
	Reconciliation(context.Context, id.ID, id.ID) (domain.Reconciliation, error)
}

type Accounts struct{ reader AccountReader }

func NewAccounts(reader AccountReader) *Accounts {
	if reader == nil {
		panic("event: NewAccounts with a nil reader")
	}
	return &Accounts{reader: reader}
}

func (a *Accounts) List(ctx context.Context, workspace, event id.ID) (domain.AccountPage, error) {
	if workspace.IsZero() || event.IsZero() {
		return domain.AccountPage{}, domain.ErrIDRequired
	}
	items, err := a.reader.Accounts(ctx, workspace, event)
	if err != nil {
		return domain.AccountPage{}, err
	}
	if items == nil {
		items = []domain.Account{}
	}
	reconciliation, err := a.reader.Reconciliation(ctx, workspace, event)
	if err != nil && err != domain.ErrNotFound {
		return domain.AccountPage{}, err
	}
	var selected *domain.Reconciliation
	if err == nil {
		selected = &reconciliation
	}
	return domain.AccountPage{Items: items, Reconciliation: selected}, nil
}
