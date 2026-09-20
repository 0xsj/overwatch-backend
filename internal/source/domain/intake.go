package domain

import (
	"net/url"
	"strings"
	"time"

	"github.com/0xsj/overwatch-backend/pkg/errors"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

const (
	EventIntakeCreated  = "source.intake.created"
	EventIntakeApproved = "source.intake.approved"
	EventIntakeRejected = "source.intake.rejected"

	IntakePending   = "pending"
	IntakeApproved  = "approved"
	IntakeRejected  = "rejected"
	IntakeReference = "reference"
	IntakeImport    = "import"
)

var (
	ErrIntakeInvalid         = errors.New(errors.Invalid, "source intake candidate is invalid")
	ErrIntakeAlreadyReviewed = errors.New(errors.Conflict, "source intake candidate has already been reviewed")
	ErrIntakeReviewInvalid   = errors.New(errors.Invalid, "source intake review requires a valid decision and note")
)

type IntakeCandidate struct {
	ID           id.ID      `json:"intake_id"`
	WorkspaceID  id.ID      `json:"workspace_id"`
	Title        string     `json:"title"`
	Origin       string     `json:"origin"`
	URL          string     `json:"url"`
	Filename     string     `json:"filename,omitempty"`
	MediaType    string     `json:"media_type,omitempty"`
	ContentBytes []byte     `json:"-"`
	Note         string     `json:"note,omitempty"`
	CreatedBy    id.ID      `json:"created_by"`
	CreatedAt    time.Time  `json:"created_at"`
	Status       string     `json:"status"`
	ReviewedBy   id.ID      `json:"reviewed_by,omitempty"`
	ReviewedAt   *time.Time `json:"reviewed_at,omitempty"`
	ReviewNote   string     `json:"review_note,omitempty"`
	SourceID     id.ID      `json:"source_id,omitempty"`
}

func NewIntakeCandidate(want, workspace, author id.ID, title, address, note string, at time.Time) (IntakeCandidate, error) {
	if want.IsZero() || workspace.IsZero() || author.IsZero() || at.IsZero() {
		return IntakeCandidate{}, ErrIntakeInvalid
	}
	title = strings.TrimSpace(title)
	address = strings.TrimSpace(address)
	note = strings.TrimSpace(note)
	if !metadata(title, 400) || title == "" || !metadata(address, 4000) || !metadata(note, 2000) || address == "" {
		return IntakeCandidate{}, ErrIntakeInvalid
	}
	parsed, err := url.Parse(address)
	if err != nil || parsed.Hostname() == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.User != nil {
		return IntakeCandidate{}, ErrIntakeInvalid
	}
	return IntakeCandidate{ID: want, WorkspaceID: workspace, Title: title, Origin: IntakeReference, URL: address, Note: note, CreatedBy: author, CreatedAt: at, Status: IntakePending}, nil
}

func NewImportIntakeCandidate(want, workspace, author id.ID, title, filename, mediaType string, content []byte, note string, at time.Time) (IntakeCandidate, error) {
	if want.IsZero() || workspace.IsZero() || author.IsZero() || at.IsZero() {
		return IntakeCandidate{}, ErrIntakeInvalid
	}
	title = strings.TrimSpace(title)
	filename = strings.TrimSpace(filename)
	note = strings.TrimSpace(note)
	if !metadata(title, 400) || title == "" || !metadata(filename, 400) || filename == "" || !metadata(note, 2000) || len(content) == 0 || len(content) > MaxBinaryCaptureBytes {
		return IntakeCandidate{}, ErrIntakeInvalid
	}
	if strings.HasPrefix(mediaType, "image/") || mediaType == "application/pdf" {
		if err := ValidateBinary(mediaType, content); err != nil {
			return IntakeCandidate{}, err
		}
	} else {
		normalized, err := ValidateContent(mediaType, string(content))
		if err != nil {
			return IntakeCandidate{}, err
		}
		if normalized == "" {
			return IntakeCandidate{}, ErrIntakeInvalid
		}
		mediaType = normalized
	}
	return IntakeCandidate{ID: want, WorkspaceID: workspace, Title: title, Origin: IntakeImport, Filename: filename, MediaType: mediaType, ContentBytes: append([]byte(nil), content...), Note: note, CreatedBy: author, CreatedAt: at, Status: IntakePending}, nil
}

func (c IntakeCandidate) Review(by id.ID, decision, note string, source id.ID, at time.Time) (IntakeCandidate, error) {
	if by.IsZero() || at.IsZero() || c.Status != IntakePending {
		if c.Status != IntakePending {
			return c, ErrIntakeAlreadyReviewed
		}
		return c, ErrIntakeReviewInvalid
	}
	decision = strings.TrimSpace(decision)
	note = strings.TrimSpace(note)
	if (decision != IntakeApproved && decision != IntakeRejected) || note == "" || !metadata(note, 2000) {
		return c, ErrIntakeReviewInvalid
	}
	if decision == IntakeApproved && source.IsZero() {
		return c, ErrIntakeReviewInvalid
	}
	if decision == IntakeRejected && !source.IsZero() {
		return c, ErrIntakeReviewInvalid
	}
	next := c
	next.Status = decision
	next.ReviewedBy = by
	next.ReviewedAt = &at
	next.ReviewNote = note
	next.SourceID = source
	return next, nil
}
