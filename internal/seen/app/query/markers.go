package query

import (
	"context"
	"time"

	"github.com/0xsj/overwatch-backend/internal/seen/domain"
	"github.com/0xsj/overwatch-backend/pkg/errors"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

type Reader interface {
	ByAccountWorkspace(ctx context.Context, account, workspace id.ID) (domain.Marker, error)
}

type Markers struct{ reader Reader }

func NewMarkers(reader Reader) *Markers {
	if reader == nil {
		panic("seen: NewMarkers with a nil reader")
	}
	return &Markers{reader: reader}
}

// ForAccount answers the watermark, with a zero time meaning that this reader
// has never marked the view seen. Missing is not an error on a first visit.
func (m *Markers) ForAccount(ctx context.Context, account, workspace id.ID) (time.Time, error) {
	if account.IsZero() {
		return time.Time{}, domain.ErrIDRequired
	}
	if workspace.IsZero() {
		return time.Time{}, domain.ErrWorkspaceRequired
	}
	found, err := m.reader.ByAccountWorkspace(ctx, account, workspace)
	if err != nil {
		if errors.IsKind(err, errors.NotFound) {
			return time.Time{}, nil
		}
		return time.Time{}, err
	}
	return found.SeenAt, nil
}
