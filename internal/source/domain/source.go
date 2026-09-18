// Package domain owns retained research material. A source describes where
// material came from; a capture names immutable bytes independently of tools.
package domain

import (
	"encoding/json"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/0xsj/overwatch-backend/pkg/blob"
	"github.com/0xsj/overwatch-backend/pkg/errors"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

const MaxCaptureBytes = 256 * 1024
const MaxBinaryCaptureBytes = 8 << 20
const (
	EventCreated          = "source.created"
	EventCaptured         = "source.captured"
	EventRetentionChanged = "source.retention.changed"
	EventPrivacyChanged   = "source.privacy.changed"
	EventPurged           = "source.purged"

	SensitivityPublic     = "public"
	SensitivityInternal   = "internal"
	SensitivityRestricted = "restricted"

	RetentionUnscheduled = "unscheduled"
	RetentionScheduled   = "scheduled"
	RetentionDue         = "due"
	RetentionBlocked     = "blocked"
	RetentionHeld        = "held"
	RetentionPurged      = "purged"
)

var (
	ErrInvalid               = errors.New(errors.Invalid, "invalid source")
	ErrNotFound              = errors.New(errors.NotFound, "source or capture")
	ErrContent               = errors.New(errors.Invalid, "capture requires nonempty UTF-8 text up to 256 KiB; JSON captures must contain valid JSON")
	ErrRetentionInvalid      = errors.New(errors.Invalid, "retention must be cleared or set to a valid timestamp")
	ErrPrivacyInvalid        = errors.New(errors.Invalid, "source privacy settings are invalid")
	ErrPurgeInvalid          = errors.New(errors.Invalid, "purge requires a nonempty audit reason")
	ErrRetentionStateInvalid = errors.New(errors.Invalid, "retention review state is invalid")
	ErrPurged                = errors.New(errors.Conflict, "source content has been purged")
)

type Source struct {
	ID                 id.ID      `json:"source_id"`
	WorkspaceID        id.ID      `json:"workspace_id"`
	Title              string     `json:"title"`
	Origin             string     `json:"origin"`
	URL                string     `json:"url,omitempty"`
	Filename           string     `json:"filename,omitempty"`
	CreatedBy          id.ID      `json:"created_by"`
	CreatedAt          time.Time  `json:"created_at"`
	RetentionUntil     *time.Time `json:"retention_until,omitempty"`
	RetentionUpdatedBy id.ID      `json:"retention_updated_by,omitempty"`
	RetentionUpdatedAt *time.Time `json:"retention_updated_at,omitempty"`
	Sensitivity        string     `json:"sensitivity"`
	PrivacyUpdatedBy   id.ID      `json:"privacy_updated_by,omitempty"`
	PrivacyUpdatedAt   *time.Time `json:"privacy_updated_at,omitempty"`
	LegalHold          bool       `json:"legal_hold"`
	LegalHoldReason    string     `json:"legal_hold_reason,omitempty"`
	PurgedAt           *time.Time `json:"purged_at,omitempty"`
	PurgedBy           id.ID      `json:"purged_by,omitempty"`
	PurgeReason        string     `json:"purge_reason,omitempty"`
}

type Capture struct {
	ID          id.ID     `json:"capture_id"`
	SourceID    id.ID     `json:"source_id"`
	WorkspaceID id.ID     `json:"-"`
	Version     int       `json:"version"`
	MediaType   string    `json:"media_type"`
	SHA256      string    `json:"sha256"`
	Bytes       int64     `json:"bytes"`
	CapturedBy  id.ID     `json:"captured_by"`
	CapturedAt  time.Time `json:"captured_at"`
}

// SearchRow is the metadata needed to reopen one indexed text artifact. The
// searchable body remains in content-addressed storage; this row never becomes
// a second source of truth for retained text.
type SearchRow struct {
	ID               id.ID
	WorkspaceID      id.ID
	SourceID         id.ID
	SourceTitle      string
	CaptureID        id.ID
	CaptureVersion   int
	MediaType        string
	ExtractionID     id.ID
	ExtractionMethod string
	ContentSHA256    string
	ContentBytes     int64
}

type Summary struct {
	Source
	LatestCapture *Capture `json:"latest_capture"`
}

type Draft struct {
	Title        string  `json:"title"`
	Origin       string  `json:"origin"`
	URL          string  `json:"url,omitempty"`
	Filename     string  `json:"filename,omitempty"`
	MediaType    string  `json:"media_type,omitempty"`
	Content      *string `json:"content,omitempty"`
	ContentBytes []byte  `json:"content_base64,omitempty"`
}

func New(want, workspace, author id.ID, in Draft, at time.Time) (Source, error) {
	if want.IsZero() || workspace.IsZero() || author.IsZero() || at.IsZero() {
		return Source{}, ErrInvalid
	}
	title, filename, address := strings.TrimSpace(in.Title), strings.TrimSpace(in.Filename), strings.TrimSpace(in.URL)
	if !metadata(title, 400) || title == "" || !metadata(filename, 400) || !metadata(address, 4000) {
		return Source{}, ErrInvalid
	}
	if address != "" {
		u, err := url.Parse(address)
		if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Hostname() == "" || u.User != nil {
			return Source{}, errors.New(errors.Invalid, "source URL must be HTTP or HTTPS without credentials")
		}
	}
	switch in.Origin {
	case "reference":
		if address == "" || in.Content != nil || len(in.ContentBytes) > 0 {
			return Source{}, errors.New(errors.Invalid, "a reference requires a URL and no initial capture")
		}
	case "paste":
		if in.Content == nil || len(in.ContentBytes) > 0 {
			return Source{}, errors.New(errors.Invalid, "a paste requires text content")
		}
	case "import":
		if (in.Content == nil && len(in.ContentBytes) == 0) || filename == "" {
			return Source{}, errors.New(errors.Invalid, "paste and import require content; import also requires a filename")
		}
	default:
		return Source{}, errors.New(errors.Invalid, "source origin must be paste, import, or reference")
	}
	if in.Content != nil {
		if _, err := ValidateContent(in.MediaType, *in.Content); err != nil {
			return Source{}, err
		}
	}
	if len(in.ContentBytes) > 0 {
		if err := ValidateBinary(in.MediaType, in.ContentBytes); err != nil {
			return Source{}, err
		}
	}
	return Source{ID: want, WorkspaceID: workspace, Title: title, Origin: in.Origin, URL: address, Filename: filename, CreatedBy: author, CreatedAt: at, Sensitivity: SensitivityInternal}, nil
}

func (s Source) SetRetention(by id.ID, until *time.Time, at time.Time) (Source, error) {
	if by.IsZero() || at.IsZero() {
		return s, ErrInvalid
	}
	if until != nil && until.IsZero() {
		return s, ErrRetentionInvalid
	}
	next := s
	next.RetentionUntil = until
	next.RetentionUpdatedBy = by
	next.RetentionUpdatedAt = &at
	return next, nil
}

func (s Source) SetPrivacy(by id.ID, sensitivity string, legalHold bool, reason string, at time.Time) (Source, error) {
	if by.IsZero() || at.IsZero() || !validSensitivity(sensitivity) {
		return s, ErrPrivacyInvalid
	}
	reason = strings.TrimSpace(reason)
	if !metadata(reason, 2000) || (!legalHold && reason != "") || (legalHold && reason == "") {
		return s, ErrPrivacyInvalid
	}
	next := s
	next.Sensitivity = sensitivity
	next.LegalHold = legalHold
	next.LegalHoldReason = reason
	next.PrivacyUpdatedBy = by
	next.PrivacyUpdatedAt = &at
	return next, nil
}

type PurgeDependencies struct {
	Captures             int `json:"capture_count"`
	ManualObservations   int `json:"observation_count"`
	Extractions          int `json:"extraction_count"`
	AssistanceOperations int `json:"assistance_operation_count"`
	AssistanceProposals  int `json:"assistance_proposal_count"`
}

type PurgeReview struct {
	SourceID        id.ID             `json:"source_id"`
	State           string            `json:"state"`
	RetentionUntil  *time.Time        `json:"retention_until,omitempty"`
	Sensitivity     string            `json:"sensitivity"`
	LegalHold       bool              `json:"legal_hold"`
	LegalHoldReason string            `json:"legal_hold_reason,omitempty"`
	Dependencies    PurgeDependencies `json:"dependencies"`
	Eligible        bool              `json:"eligible"`
	Blockers        []string          `json:"blockers"`
}

type RetentionQueueItem struct {
	Source Source      `json:"source"`
	Review PurgeReview `json:"review"`
}

func (s Source) ReviewPurge(now time.Time, dependencies PurgeDependencies) PurgeReview {
	review := PurgeReview{
		SourceID: s.ID, RetentionUntil: s.RetentionUntil, Sensitivity: s.Sensitivity,
		LegalHold: s.LegalHold, LegalHoldReason: s.LegalHoldReason,
		Dependencies: dependencies, Blockers: []string{},
	}
	switch {
	case s.PurgedAt != nil:
		review.State = RetentionPurged
		review.Blockers = append(review.Blockers, "already_purged")
	case s.LegalHold:
		review.State = RetentionHeld
	case s.RetentionUntil == nil:
		review.State = RetentionUnscheduled
	case now.Before(*s.RetentionUntil):
		review.State = RetentionScheduled
	default:
		review.State = RetentionDue
	}
	if s.PurgedAt != nil {
		return review
	}
	if s.LegalHold {
		review.Blockers = append(review.Blockers, "legal_hold")
	}
	if s.RetentionUntil == nil || now.Before(*s.RetentionUntil) {
		review.Blockers = append(review.Blockers, "retention_not_due")
	}
	if dependencies.ManualObservations > 0 {
		review.Blockers = append(review.Blockers, "observations_present")
	}
	if dependencies.Extractions > 0 {
		review.Blockers = append(review.Blockers, "extractions_present")
	}
	if dependencies.AssistanceOperations > 0 || dependencies.AssistanceProposals > 0 {
		review.Blockers = append(review.Blockers, "assistance_present")
	}
	if review.State == RetentionDue && len(review.Blockers) > 0 {
		review.State = RetentionBlocked
	}
	review.Eligible = len(review.Blockers) == 0
	return review
}

func (s Source) Purge(by id.ID, reason string, at time.Time) (Source, error) {
	if by.IsZero() || at.IsZero() {
		return s, ErrPurgeInvalid
	}
	reason = strings.TrimSpace(reason)
	if !metadata(reason, 2000) || reason == "" {
		return s, ErrPurgeInvalid
	}
	next := s
	next.PurgedAt = &at
	next.PurgedBy = by
	next.PurgeReason = reason
	return next, nil
}

func validSensitivity(value string) bool {
	return value == SensitivityPublic || value == SensitivityInternal || value == SensitivityRestricted
}

func metadata(s string, limit int) bool {
	return utf8.ValidString(s) && !strings.ContainsRune(s, 0) && len(s) <= limit
}

func ValidateContent(mediaType, content string) (string, error) {
	if mediaType == "" {
		mediaType = "text/plain"
	}
	if content == "" || len(content) > MaxCaptureBytes || !utf8.ValidString(content) {
		return "", ErrContent
	}
	switch mediaType {
	case "text/plain", "text/html":
	case "application/json":
		if !json.Valid([]byte(content)) {
			return "", ErrContent
		}
	default:
		return "", errors.New(errors.Invalid, "capture media type must be text/plain or application/json")
	}
	return mediaType, nil
}

func ValidateBinary(mediaType string, content []byte) error {
	if len(content) == 0 || len(content) > MaxBinaryCaptureBytes {
		return ErrContent
	}
	switch mediaType {
	case "application/pdf":
		if len(content) < 5 || string(content[:5]) != "%PDF-" {
			return errors.New(errors.Invalid, "PDF capture does not have a PDF signature")
		}
	case "image/png":
		if len(content) < 8 || string(content[:8]) != "\x89PNG\r\n\x1a\n" {
			return errors.New(errors.Invalid, "PNG capture does not have a PNG signature")
		}
	case "image/jpeg":
		if len(content) < 3 || content[0] != 0xff || content[1] != 0xd8 || content[2] != 0xff {
			return errors.New(errors.Invalid, "JPEG capture does not have a JPEG signature")
		}
	case "image/webp":
		if len(content) < 12 || string(content[:4]) != "RIFF" || string(content[8:12]) != "WEBP" {
			return errors.New(errors.Invalid, "WebP capture does not have a WebP signature")
		}
	default:
		return errors.New(errors.Invalid, "binary capture media type must be PDF, PNG, JPEG, or WebP")
	}
	return nil
}

func binaryMediaType(mediaType string) bool {
	switch mediaType {
	case "application/pdf", "image/png", "image/jpeg", "image/webp":
		return true
	default:
		return false
	}
}

func NewCapture(want, source, workspace, author id.ID, version int, mediaType string, info blob.Info, at time.Time) (Capture, error) {
	if want.IsZero() || source.IsZero() || workspace.IsZero() || author.IsZero() || at.IsZero() || version < 1 {
		return Capture{}, ErrInvalid
	}
	if mediaType != "text/plain" && mediaType != "text/html" && mediaType != "application/json" && !binaryMediaType(mediaType) {
		return Capture{}, ErrContent
	}
	if _, err := blob.ParseRef(info.Ref.String()); err != nil || info.Size <= 0 || (!binaryMediaType(mediaType) && info.Size > MaxCaptureBytes) || info.Size > MaxBinaryCaptureBytes {
		return Capture{}, ErrContent
	}
	return Capture{ID: want, SourceID: source, WorkspaceID: workspace, Version: version, MediaType: mediaType, SHA256: info.Ref.Hex, Bytes: info.Size, CapturedBy: author, CapturedAt: at}, nil
}
