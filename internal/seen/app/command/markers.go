package command

import (
	"context"

	"github.com/0xsj/overwatch-backend/internal/seen/domain"
	"github.com/0xsj/overwatch-backend/pkg/clock"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

type Repository interface {
	Upsert(ctx context.Context, marker domain.Marker) error
}

type Markers struct {
	repo  Repository
	clock clock.Clock
}

func NewMarkers(repo Repository, clk clock.Clock) *Markers {
	if repo == nil || clk == nil {
		panic("seen: NewMarkers with a nil dependency")
	}
	return &Markers{repo: repo, clock: clk}
}

// Mark uses server time. The client supplies no timestamp, so one account
// cannot move its watermark backwards or make another account's view appear
// read by posting a fabricated instant.
func (m *Markers) Mark(ctx context.Context, account, workspace id.ID) (domain.Marker, error) {
	fresh, err := domain.New(account, workspace, m.clock.Now())
	if err != nil {
		return domain.Marker{}, err
	}
	if err := m.repo.Upsert(ctx, fresh); err != nil {
		return domain.Marker{}, err
	}
	return fresh, nil
}
