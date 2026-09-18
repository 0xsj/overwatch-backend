// Package domain owns provenance-linked text derived from an immutable source
// capture. A derived extraction is never a replacement for the capture.
package domain

import (
	"strings"
	"time"
	"unicode/utf8"

	"github.com/0xsj/overwatch-backend/pkg/errors"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

const (
	MaxTextBytes = 8 << 20
	MethodPDFV1  = "pdf-text-v1"
	MethodOCRV1  = "ocr-v1"

	EventCreated = "source.extraction.created"
)

type Status string

const (
	Succeeded   Status = "succeeded"
	Unsupported Status = "unsupported"
	Failed      Status = "failed"
)

func (s Status) String() string { return string(s) }

func ParseStatus(raw string) (Status, error) {
	switch Status(raw) {
	case Succeeded, Unsupported, Failed:
		return Status(raw), nil
	default:
		return "", ErrStatusUnknown
	}
}

var (
	ErrIDRequired        = errors.New(errors.Invalid, "an extraction identifier is required")
	ErrWorkspaceRequired = errors.New(errors.Invalid, "an extraction belongs to an investigation")
	ErrMethodRequired    = errors.New(errors.Invalid, "an extraction method is required")
	ErrStatusUnknown     = errors.New(errors.Invalid, "not an extraction status this system knows")
	ErrTextInvalid       = errors.New(errors.Invalid, "extracted text must be nonempty UTF-8 up to 8 MiB")
	ErrMessageRequired   = errors.New(errors.Invalid, "a failed extraction needs an explanation")
	ErrUnsupported       = errors.New(errors.Invalid, "this capture format has no extraction method yet")
	ErrNotFound          = errors.New(errors.NotFound, "source extraction")
)

// Extraction is the durable result of one attempt to derive text from one
// exact capture. OutputHash names bytes in pkg/blob; an empty hash means the
// attempt produced no text and must have an explanation instead.
type Extraction struct {
	ID          id.ID     `json:"extraction_id"`
	WorkspaceID id.ID     `json:"workspace_id"`
	SourceID    id.ID     `json:"source_id"`
	CaptureID   id.ID     `json:"capture_id"`
	Method      string    `json:"method"`
	Status      Status    `json:"status"`
	OutputHash  string    `json:"output_sha256,omitempty"`
	OutputBytes int64     `json:"output_bytes"`
	Message     string    `json:"message,omitempty"`
	CreatedBy   id.ID     `json:"created_by"`
	CreatedAt   time.Time `json:"created_at"`
}

func New(want, workspace, source, capture, author id.ID, method string, status Status, outputHash string, outputBytes int64, message string, at time.Time) (Extraction, error) {
	if want.IsZero() || source.IsZero() || capture.IsZero() || author.IsZero() {
		return Extraction{}, ErrIDRequired
	}
	if workspace.IsZero() {
		return Extraction{}, ErrWorkspaceRequired
	}
	if strings.TrimSpace(method) == "" {
		return Extraction{}, ErrMethodRequired
	}
	parsed, err := ParseStatus(status.String())
	if err != nil {
		return Extraction{}, err
	}
	if at.IsZero() {
		return Extraction{}, errors.New(errors.Invalid, "an extraction needs a timestamp")
	}
	message = strings.TrimSpace(message)
	if parsed == Succeeded {
		if outputHash == "" || outputBytes <= 0 || outputBytes > MaxTextBytes || message != "" {
			return Extraction{}, ErrTextInvalid
		}
	} else {
		if outputHash != "" || outputBytes != 0 {
			return Extraction{}, ErrTextInvalid
		}
		if message == "" || !utf8.ValidString(message) || strings.ContainsRune(message, 0) {
			return Extraction{}, ErrMessageRequired
		}
	}
	return Extraction{ID: want, WorkspaceID: workspace, SourceID: source, CaptureID: capture, Method: method, Status: parsed, OutputHash: outputHash, OutputBytes: outputBytes, Message: message, CreatedBy: author, CreatedAt: at}, nil
}
