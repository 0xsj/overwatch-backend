package domain

import (
	"strings"
	"time"
	"unicode/utf8"

	"github.com/0xsj/overwatch-backend/pkg/errors"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

const EventManualRecorded = "observation.manual.recorded"

var ErrCitation = errors.New(errors.Invalid, "citation must match an exact nonempty passage in the retained capture")
var ErrStatement = errors.New(errors.Invalid, "statement requires UTF-8 text up to 4000 bytes; locator allows 400 bytes")

// Manual is an observation origin with no tool invocation or mapping. Its
// immutable citation identifies the precise capture version and code-point
// range; editable analyst interpretation belongs in working notes.
type Manual struct {
	ID           id.ID     `json:"observation_id"`
	WorkspaceID  id.ID     `json:"workspace_id"`
	SourceID     id.ID     `json:"source_id"`
	CaptureID    id.ID     `json:"capture_id"`
	ExtractionID *id.ID    `json:"extraction_id,omitempty"`
	Statement    string    `json:"statement"`
	Quote        string    `json:"quote"`
	QuoteStart   int       `json:"quote_start"`
	QuoteEnd     int       `json:"quote_end"`
	Locator      string    `json:"locator,omitempty"`
	Author       id.ID     `json:"author"`
	RecordedAt   time.Time `json:"recorded_at"`
	Origin       string    `json:"origin"`
}

func NewManual(want, workspace, source, capture, author id.ID, statement, quote, locator, content string, start *int, at time.Time) (Manual, error) {
	if want.IsZero() || workspace.IsZero() || source.IsZero() || capture.IsZero() || author.IsZero() {
		return Manual{}, ErrIDRequired
	}
	if at.IsZero() {
		return Manual{}, ErrTimeRequired
	}
	statement, locator = strings.TrimSpace(statement), strings.TrimSpace(locator)
	if statement == "" || len(statement) > 4000 || len(locator) > 400 || !utf8.ValidString(statement) || !utf8.ValidString(locator) || strings.ContainsRune(statement, 0) || strings.ContainsRune(locator, 0) {
		return Manual{}, ErrStatement
	}
	if quote == "" || len(quote) > 8000 || !utf8.ValidString(quote) || !utf8.ValidString(content) {
		return Manual{}, ErrCitation
	}
	offset := 0
	if start == nil {
		found := strings.Index(content, quote)
		if found < 0 {
			return Manual{}, ErrCitation
		}
		offset = utf8.RuneCountInString(content[:found])
	} else {
		offset = *start
	}
	runes := []rune(content)
	length := utf8.RuneCountInString(quote)
	// Subtraction avoids integer overflow from an untrusted offset.
	if offset < 0 || offset > len(runes) || length > len(runes)-offset || string(runes[offset:offset+length]) != quote {
		return Manual{}, ErrCitation
	}
	return Manual{ID: want, WorkspaceID: workspace, SourceID: source, CaptureID: capture, Statement: statement, Quote: quote, QuoteStart: offset, QuoteEnd: offset + length, Locator: locator, Author: author, RecordedAt: at, Origin: "manual"}, nil
}
