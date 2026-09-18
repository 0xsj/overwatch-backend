package query

import (
	"context"
	"errors"

	"github.com/0xsj/overwatch-backend/internal/assistance/domain"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

type Reader interface {
	ByOperation(context.Context, id.ID, id.ID) (domain.Operation, error)
	ByCapture(context.Context, id.ID, id.ID, id.ID, id.ID, int) ([]domain.Operation, error)
	LatestByCapture(context.Context, id.ID, id.ID, id.ID, id.ID) (domain.Operation, error)
	Proposals(context.Context, id.ID, id.ID) ([]domain.Proposal, error)
}

type Operations struct{ reader Reader }

func NewOperations(reader Reader) *Operations {
	if reader == nil {
		panic("assistance: NewOperations with a nil reader")
	}
	return &Operations{reader: reader}
}

type Detail struct {
	Operation domain.Operation  `json:"operation"`
	Proposals []domain.Proposal `json:"proposals"`
}

// LatestDetail is nullable by design: a capture can have no assistance
// operation yet, which is different from a failed read or a missing capture.
type LatestDetail struct {
	Operation *domain.Operation `json:"operation"`
	Proposals []domain.Proposal `json:"proposals"`
}

type HistoryPage struct {
	Items []Detail `json:"items"`
}

func (o *Operations) ByID(ctx context.Context, workspace, operation id.ID) (Detail, error) {
	if workspace.IsZero() || operation.IsZero() {
		return Detail{}, domain.ErrIDRequired
	}
	found, err := o.reader.ByOperation(ctx, workspace, operation)
	if err != nil {
		return Detail{}, err
	}
	proposals, err := o.reader.Proposals(ctx, workspace, operation)
	if err != nil {
		return Detail{}, err
	}
	if proposals == nil {
		proposals = []domain.Proposal{}
	}
	return Detail{Operation: found, Proposals: proposals}, nil
}

func (o *Operations) Latest(ctx context.Context, workspace, source, capture, extraction id.ID) (LatestDetail, error) {
	if workspace.IsZero() || source.IsZero() || capture.IsZero() {
		return LatestDetail{}, domain.ErrIDRequired
	}
	found, err := o.reader.LatestByCapture(ctx, workspace, source, capture, extraction)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return LatestDetail{Proposals: []domain.Proposal{}}, nil
		}
		return LatestDetail{}, err
	}
	proposals, err := o.reader.Proposals(ctx, workspace, found.ID)
	if err != nil {
		return LatestDetail{}, err
	}
	if proposals == nil {
		proposals = []domain.Proposal{}
	}
	return LatestDetail{Operation: &found, Proposals: proposals}, nil
}

func (o *Operations) History(ctx context.Context, workspace, source, capture, extraction id.ID, limit int) (HistoryPage, error) {
	if workspace.IsZero() || source.IsZero() || capture.IsZero() {
		return HistoryPage{}, domain.ErrIDRequired
	}
	if limit <= 0 {
		limit = 20
	}
	if limit > 50 {
		limit = 50
	}
	operations, err := o.reader.ByCapture(ctx, workspace, source, capture, extraction, limit)
	if err != nil {
		return HistoryPage{}, err
	}
	items := make([]Detail, 0, len(operations))
	for _, operation := range operations {
		proposals, err := o.reader.Proposals(ctx, workspace, operation.ID)
		if err != nil {
			return HistoryPage{}, err
		}
		if proposals == nil {
			proposals = []domain.Proposal{}
		}
		items = append(items, Detail{Operation: operation, Proposals: proposals})
	}
	return HistoryPage{Items: items}, nil
}
