package query

import (
	"context"
	"io"

	"github.com/0xsj/overwatch-backend/internal/report/domain"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

type Reader interface {
	ByID(ctx context.Context, workspace, want id.ID) (domain.Report, error)
	Page(ctx context.Context, workspace, target id.ID, limit int) ([]domain.Report, error)
	Revisions(ctx context.Context, report id.ID) ([]domain.Revision, error)
	RevisionByID(ctx context.Context, workspace, want id.ID) (domain.Revision, error)
}

// Bytes is `pkg/blob`'s read side. A separate port from the row reader because
// the two fail differently: a missing row is a document that never existed, and
// a missing blob is a delivered document whose bytes are gone — which is a much
// louder problem, and the one owed item Q will eventually cause.
type Bytes interface {
	Open(ctx context.Context, hash string) (io.ReadCloser, error)
}

const (
	DefaultPage = 50
	MaxPage     = 200
)

type Reports struct {
	reader Reader
	bytes  Bytes
}

func NewReports(reader Reader, bytes Bytes) *Reports {
	if reader == nil || bytes == nil {
		panic("report: NewReports with a nil dependency")
	}
	return &Reports{reader: reader, bytes: bytes}
}

func page(limit int) int {
	if limit <= 0 {
		return DefaultPage
	}
	if limit > MaxPage {
		return MaxPage
	}
	return limit
}

func (r *Reports) ByID(ctx context.Context, workspace, want id.ID) (domain.Report, error) {
	if workspace.IsZero() || want.IsZero() {
		return domain.Report{}, domain.ErrIDRequired
	}
	return r.reader.ByID(ctx, workspace, want)
}

func (r *Reports) List(ctx context.Context, workspace, target id.ID, limit int) ([]domain.Report, error) {
	if workspace.IsZero() {
		return nil, domain.ErrWorkspaceRequired
	}
	return r.reader.Page(ctx, workspace, target, page(limit))
}

// Detail is a report and everything issued from it.
type Detail struct {
	Report    domain.Report
	Revisions []domain.Revision
}

func (r *Reports) Detail(ctx context.Context, workspace, want id.ID) (Detail, error) {
	found, err := r.ByID(ctx, workspace, want)
	if err != nil {
		return Detail{}, err
	}
	revisions, err := r.reader.Revisions(ctx, found.ID)
	if err != nil {
		return Detail{}, err
	}
	return Detail{Report: found, Revisions: revisions}, nil
}

// Open hands back the FROZEN BYTES of one revision, having first read the ROW —
// which is the tenancy check. Reaching `pkg/blob` with a hash alone would let
// anybody who guessed a content address read another client's report, and a
// content address is exactly the kind of thing that ends up in a log.
func (r *Reports) Open(ctx context.Context, workspace, revision id.ID) (domain.Revision, io.ReadCloser, error) {
	if workspace.IsZero() || revision.IsZero() {
		return domain.Revision{}, nil, domain.ErrIDRequired
	}
	found, err := r.reader.RevisionByID(ctx, workspace, revision)
	if err != nil {
		return domain.Revision{}, nil, err
	}
	body, err := r.bytes.Open(ctx, found.Hash)
	if err != nil {
		return domain.Revision{}, nil, err
	}
	return found, body, nil
}
