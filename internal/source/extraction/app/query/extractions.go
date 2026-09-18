package query

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"

	"github.com/0xsj/overwatch-backend/internal/source/extraction/domain"
	"github.com/0xsj/overwatch-backend/pkg/blob"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

type Reader interface {
	Page(context.Context, id.ID, id.ID, id.ID, id.ID, int) ([]domain.Extraction, error)
	ByID(context.Context, id.ID, id.ID, id.ID, id.ID) (domain.Extraction, error)
}
type Blobs interface {
	Open(context.Context, blob.Ref) (io.ReadCloser, error)
}
type Extractions struct {
	reader Reader
	blobs  Blobs
}

func NewExtractions(reader Reader, blobs Blobs) *Extractions {
	if reader == nil || blobs == nil {
		panic("source extraction: NewExtractions with a nil dependency")
	}
	return &Extractions{reader: reader, blobs: blobs}
}

type Page struct {
	Items      []domain.Extraction `json:"items"`
	NextCursor *id.ID              `json:"next_cursor"`
}
type Detail struct {
	domain.Extraction
	Text string `json:"text,omitempty"`
}

func (e *Extractions) List(ctx context.Context, workspace, source, capture, before id.ID, limit int) (Page, error) {
	if workspace.IsZero() || source.IsZero() || capture.IsZero() {
		return Page{}, domain.ErrIDRequired
	}
	if limit <= 0 {
		limit = 50
	}
	if limit > 100 {
		limit = 100
	}
	rows, err := e.reader.Page(ctx, workspace, source, capture, before, limit+1)
	if err != nil {
		return Page{}, err
	}
	out := Page{Items: rows}
	if out.Items == nil {
		out.Items = []domain.Extraction{}
	}
	if len(rows) > limit {
		cursor := rows[limit-1].ID
		out.NextCursor = &cursor
		out.Items = rows[:limit]
	}
	return out, nil
}

func (e *Extractions) Detail(ctx context.Context, workspace, source, capture, extraction id.ID) (Detail, error) {
	if workspace.IsZero() || source.IsZero() || capture.IsZero() || extraction.IsZero() {
		return Detail{}, domain.ErrIDRequired
	}
	found, err := e.reader.ByID(ctx, workspace, source, capture, extraction)
	if err != nil {
		return Detail{}, err
	}
	if found.Status != domain.Succeeded {
		return Detail{Extraction: found}, nil
	}
	ref, err := blob.ParseRef("sha256:" + found.OutputHash)
	if err != nil {
		return Detail{}, err
	}
	reader, err := e.blobs.Open(ctx, ref)
	if err != nil {
		return Detail{}, err
	}
	defer reader.Close()
	body, err := io.ReadAll(io.LimitReader(reader, domain.MaxTextBytes+1))
	if err != nil {
		return Detail{}, err
	}
	sum := sha256.Sum256(body)
	if int64(len(body)) != found.OutputBytes || hex.EncodeToString(sum[:]) != found.OutputHash || len(body) > domain.MaxTextBytes {
		return Detail{}, blob.ErrCorrupted
	}
	return Detail{Extraction: found, Text: string(body)}, nil
}
