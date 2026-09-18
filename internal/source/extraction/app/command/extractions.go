package command

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/0xsj/overwatch-backend/internal/source/extraction/domain"
	"github.com/0xsj/overwatch-backend/pkg/blob"
	"github.com/0xsj/overwatch-backend/pkg/events"
	"github.com/0xsj/overwatch-backend/pkg/id"
	"github.com/0xsj/overwatch-backend/pkg/provenance"

	pdf "github.com/ledongthuc/pdf"
)

type Repository interface {
	Create(context.Context, domain.Extraction) error
}
type Blobs interface {
	Put(context.Context, io.Reader) (blob.Info, error)
}
type TextIndexer interface {
	IndexText(context.Context, id.ID, id.ID, id.ID, id.ID, string, int64, string, time.Time) error
}
type Captures interface {
	Retained(context.Context, id.ID, id.ID, id.ID) (RetainedCapture, error)
}
type RetainedCapture struct {
	WorkspaceID id.ID
	SourceID    id.ID
	CaptureID   id.ID
	MediaType   string
	Bytes       []byte
}
type Transactor interface {
	InTx(context.Context, func(context.Context) error) error
}
type Minter interface{ NewID() id.ID }
type Clock interface{ Now() time.Time }

// ImageOCR is deliberately a small adapter boundary. The extraction command
// owns provenance and durable status; an OCR runtime only supplies text for
// the exact retained bytes it receives.
type ImageOCR interface {
	Extract(context.Context, RetainedCapture) (string, error)
}

var ErrOCRUnavailable = errors.New("image OCR is not configured in this runtime")

type UnsupportedImageOCR struct{}

func (UnsupportedImageOCR) Extract(context.Context, RetainedCapture) (string, error) {
	return "", ErrOCRUnavailable
}

type Extractions struct {
	repo      Repository
	blobs     Blobs
	captures  Captures
	tx        Transactor
	publisher events.Publisher
	ids       Minter
	clock     Clock
	ocr       ImageOCR
}

func NewExtractions(repo Repository, blobs Blobs, captures Captures, tx Transactor, publisher events.Publisher, ids Minter, clock Clock) *Extractions {
	return NewExtractionsWithOCR(repo, blobs, captures, tx, publisher, ids, clock, UnsupportedImageOCR{})
}

func NewExtractionsWithOCR(repo Repository, blobs Blobs, captures Captures, tx Transactor, publisher events.Publisher, ids Minter, clock Clock, ocr ImageOCR) *Extractions {
	if repo == nil || blobs == nil || captures == nil || tx == nil || publisher == nil || ids == nil || clock == nil {
		panic("source extraction: NewExtractions with a nil dependency")
	}
	if ocr == nil {
		panic("source extraction: NewExtractions with a nil OCR provider")
	}
	return &Extractions{repo: repo, blobs: blobs, captures: captures, tx: tx, publisher: publisher, ids: ids, clock: clock, ocr: ocr}
}

func (e *Extractions) Extract(ctx context.Context, workspace, source, capture, author id.ID) (domain.Extraction, error) {
	if workspace.IsZero() || source.IsZero() || capture.IsZero() || author.IsZero() {
		return domain.Extraction{}, domain.ErrIDRequired
	}
	retained, err := e.captures.Retained(ctx, workspace, source, capture)
	if err != nil {
		return domain.Extraction{}, err
	}
	if retained.WorkspaceID != workspace || retained.SourceID != source || retained.CaptureID != capture {
		return domain.Extraction{}, domain.ErrNotFound
	}

	method := methodFor(retained.MediaType)
	status := domain.Succeeded
	message := ""
	var text string
	switch retained.MediaType {
	case "application/pdf":
		text, err = pdfText(retained.Bytes)
		if err != nil {
			status, message = domain.Failed, err.Error()
		}
	case "image/png", "image/jpeg", "image/webp":
		text, err = e.ocr.Extract(ctx, retained)
		if err != nil {
			status = domain.Failed
			if errors.Is(err, ErrOCRUnavailable) {
				status = domain.Unsupported
			}
			message = err.Error()
		}
	default:
		status, message = domain.Unsupported, "text extraction is not available for this capture format"
	}

	var info blob.Info
	if status == domain.Succeeded {
		if strings.TrimSpace(text) == "" || !utf8.ValidString(text) || len(text) > domain.MaxTextBytes {
			status, message = domain.Failed, "the extraction contained no usable UTF-8 text"
		} else {
			info, err = e.blobs.Put(ctx, bytes.NewReader([]byte(text)))
			if err != nil {
				return domain.Extraction{}, err
			}
		}
	}

	outputHash := ""
	outputBytes := int64(0)
	if status == domain.Succeeded {
		outputHash, outputBytes = info.Ref.Hex, info.Size
	}
	fresh, err := domain.New(e.ids.NewID(), workspace, source, capture, author, method, status, outputHash, outputBytes, message, e.clock.Now())
	if err != nil {
		return domain.Extraction{}, err
	}
	if err := e.tx.InTx(ctx, func(ctx context.Context) error {
		if err := e.repo.Create(ctx, fresh); err != nil {
			return err
		}
		if fresh.Status == domain.Succeeded {
			if indexer, ok := e.repo.(TextIndexer); ok {
				if err := indexer.IndexText(ctx, fresh.WorkspaceID, fresh.SourceID, fresh.CaptureID, fresh.ID, fresh.OutputHash, fresh.OutputBytes, text, fresh.CreatedAt); err != nil {
					return err
				}
			}
		}
		return e.publish(ctx, workspace, fresh)
	}); err != nil {
		return domain.Extraction{}, err
	}
	return fresh, nil
}

func methodFor(mediaType string) string {
	switch mediaType {
	case "application/pdf":
		return domain.MethodPDFV1
	case "image/png", "image/jpeg", "image/webp":
		return domain.MethodOCRV1
	default:
		return "unsupported-v1"
	}
}

func pdfText(content []byte) (text string, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("PDF decoder failed: %v", recovered)
			text = ""
		}
	}()
	reader, err := pdf.NewReader(bytes.NewReader(content), int64(len(content)))
	if err != nil {
		return "", fmt.Errorf("PDF decoder rejected the capture: %w", err)
	}
	out, err := reader.GetPlainText()
	if err != nil {
		return "", fmt.Errorf("PDF text extraction failed: %w", err)
	}
	body, err := io.ReadAll(io.LimitReader(out, domain.MaxTextBytes+1))
	if err != nil {
		return "", fmt.Errorf("PDF text extraction failed: %w", err)
	}
	if len(body) > domain.MaxTextBytes {
		return "", fmt.Errorf("PDF extracted text exceeds %d bytes", domain.MaxTextBytes)
	}
	return string(body), nil
}

func (e *Extractions) publish(ctx context.Context, workspace id.ID, extraction domain.Extraction) error {
	prov, ok := provenance.Current(ctx)
	if !ok {
		prov = provenance.New(provenance.OriginRequest, e.ids)
	}
	prov, err := prov.WithTenant(workspace.String())
	if err != nil {
		return err
	}
	event, err := events.NewDecision(e.ids, e.clock, domain.EventCreated, "workspace:"+workspace.String(), prov, map[string]any{
		"workspace_id": workspace.String(), "source_id": extraction.SourceID.String(), "capture_id": extraction.CaptureID.String(),
		"extraction_id": extraction.ID.String(), "method": extraction.Method, "status": extraction.Status.String(),
		"output_sha256": extraction.OutputHash, "output_bytes": extraction.OutputBytes, "created_by": extraction.CreatedBy.String(),
	})
	if err != nil {
		return err
	}
	return e.publisher.Publish(ctx, event)
}
