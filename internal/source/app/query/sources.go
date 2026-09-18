package query

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"io"
	"strings"
	"time"

	"github.com/0xsj/overwatch-backend/internal/source/domain"
	"github.com/0xsj/overwatch-backend/pkg/blob"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

type Reader interface {
	Page(ctx context.Context, workspace, before id.ID, query string, limit int) ([]domain.Summary, error)
	ByID(ctx context.Context, workspace, source id.ID) (domain.Summary, error)
	Captures(ctx context.Context, workspace, source id.ID) ([]domain.Capture, error)
	Capture(ctx context.Context, workspace, source, capture id.ID) (domain.Capture, error)
	Search(ctx context.Context, workspace, before id.ID, query string, limit int) ([]domain.SearchRow, error)
}
type Blobs interface {
	Open(context.Context, blob.Ref) (io.ReadCloser, error)
}
type Sources struct {
	reader Reader
	blobs  Blobs
	clock  Clock
}

type Clock interface{ Now() time.Time }
type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now().UTC() }

func NewSources(reader Reader, blobs Blobs) *Sources {
	return NewSourcesWithClock(reader, blobs, systemClock{})
}

func NewSourcesWithClock(reader Reader, blobs Blobs, clock Clock) *Sources {
	if reader == nil || blobs == nil {
		panic("source: NewSources with a nil dependency")
	}
	if clock == nil {
		panic("source: NewSources with a nil clock")
	}
	return &Sources{reader, blobs, clock}
}

type Page struct {
	Items      []domain.Summary `json:"items"`
	NextCursor *id.ID           `json:"next_cursor"`
}
type Detail struct {
	Source   domain.Summary   `json:"source"`
	Captures []domain.Capture `json:"captures"`
}
type Captured struct {
	domain.Capture
	Content       string `json:"content,omitempty"`
	ContentBase64 string `json:"content_base64,omitempty"`
}
type SearchPage struct {
	Items      []SearchResult `json:"items"`
	NextCursor *id.ID         `json:"next_cursor"`
}
type SearchResult struct {
	SourceID         id.ID  `json:"source_id"`
	SourceTitle      string `json:"source_title"`
	CaptureID        id.ID  `json:"capture_id"`
	CaptureVersion   int    `json:"capture_version"`
	MediaType        string `json:"media_type"`
	ExtractionID     *id.ID `json:"extraction_id,omitempty"`
	ExtractionMethod string `json:"extraction_method,omitempty"`
	Excerpt          string `json:"excerpt"`
	Match            string `json:"match"`
	MatchStart       int    `json:"match_start"`
	MatchEnd         int    `json:"match_end"`
}

func (s *Sources) List(ctx context.Context, workspace, before id.ID, query string, limit int) (Page, error) {
	if workspace.IsZero() {
		return Page{}, domain.ErrInvalid
	}
	if limit <= 0 {
		limit = 50
	}
	if limit > 100 {
		limit = 100
	}
	rows, err := s.reader.Page(ctx, workspace, before, query, limit+1)
	if err != nil {
		return Page{}, err
	}
	out := Page{Items: rows}
	if out.Items == nil {
		out.Items = []domain.Summary{}
	}
	if len(rows) > limit {
		cursor := rows[limit-1].ID
		out.NextCursor = &cursor
		out.Items = rows[:limit]
	}
	return out, nil
}

func (s *Sources) Search(ctx context.Context, workspace, before id.ID, query string, limit int) (SearchPage, error) {
	if workspace.IsZero() || strings.TrimSpace(query) == "" || len(query) > 200 {
		return SearchPage{}, domain.ErrInvalid
	}
	if limit <= 0 {
		limit = 50
	}
	if limit > 100 {
		limit = 100
	}
	rows, err := s.reader.Search(ctx, workspace, before, strings.TrimSpace(query), limit+1)
	if err != nil {
		return SearchPage{}, err
	}
	pageRows := rows
	if len(rows) > limit {
		pageRows = rows[:limit]
	}
	out := SearchPage{Items: make([]SearchResult, 0, len(pageRows))}
	for _, row := range pageRows {
		content, err := s.indexedText(ctx, row)
		if err != nil {
			return SearchPage{}, err
		}
		start, end, ok := textMatch(content, query)
		if !ok {
			continue
		}
		points := []rune(content)
		const contextPoints = 96
		from, to := start-contextPoints, end+contextPoints
		if from < 0 {
			from = 0
		}
		if to > len(points) {
			to = len(points)
		}
		result := SearchResult{
			SourceID: row.SourceID, SourceTitle: row.SourceTitle, CaptureID: row.CaptureID,
			CaptureVersion: row.CaptureVersion, MediaType: row.MediaType,
			Excerpt: string(points[from:to]),
			Match:   string(points[start:end]), MatchStart: start, MatchEnd: end,
		}
		if !row.ExtractionID.IsZero() {
			extractionID := row.ExtractionID
			result.ExtractionID = &extractionID
			result.ExtractionMethod = row.ExtractionMethod
		}
		out.Items = append(out.Items, result)
	}
	if len(rows) > limit {
		cursor := rows[limit-1].ID
		out.NextCursor = &cursor
	}
	if out.Items == nil {
		out.Items = []SearchResult{}
	}
	return out, nil
}

func (s *Sources) indexedText(ctx context.Context, row domain.SearchRow) (string, error) {
	ref, err := blob.ParseRef("sha256:" + row.ContentSHA256)
	if err != nil {
		return "", err
	}
	reader, err := s.blobs.Open(ctx, ref)
	if err != nil {
		return "", err
	}
	defer reader.Close()
	body, err := io.ReadAll(io.LimitReader(reader, domain.MaxBinaryCaptureBytes+1))
	if err != nil {
		return "", err
	}
	if int64(len(body)) != row.ContentBytes || len(body) > domain.MaxBinaryCaptureBytes {
		return "", blob.ErrCorrupted
	}
	sum := sha256.Sum256(body)
	if hex.EncodeToString(sum[:]) != row.ContentSHA256 {
		return "", blob.ErrCorrupted
	}
	return string(body), nil
}

func textMatch(content, query string) (int, int, bool) {
	points := []rune(content)
	needle := []rune(strings.TrimSpace(query))
	if len(needle) == 0 || len(needle) > len(points) {
		return 0, 0, false
	}
	want := string(needle)
	for start := 0; start+len(needle) <= len(points); start++ {
		if strings.EqualFold(string(points[start:start+len(needle)]), want) {
			return start, start + len(needle), true
		}
	}
	return 0, 0, false
}
func (s *Sources) Read(ctx context.Context, workspace, source id.ID) (Detail, error) {
	if workspace.IsZero() || source.IsZero() {
		return Detail{}, domain.ErrInvalid
	}
	held, err := s.reader.ByID(ctx, workspace, source)
	if err != nil {
		return Detail{}, err
	}
	captures, err := s.reader.Captures(ctx, workspace, source)
	if err != nil {
		return Detail{}, err
	}
	if captures == nil {
		captures = []domain.Capture{}
	}
	// Use this read's capture list for latest metadata so a concurrent append
	// cannot make the header refer to a different version from the list.
	held.LatestCapture = nil
	if len(captures) > 0 {
		latest := captures[0]
		held.LatestCapture = &latest
	}
	return Detail{Source: held, Captures: captures}, nil
}
func (s *Sources) Content(ctx context.Context, workspace, source, capture id.ID) (Captured, error) {
	held, body, err := s.Bytes(ctx, workspace, source, capture)
	if err != nil {
		return Captured{}, err
	}
	if held.MediaType == "text/plain" || held.MediaType == "text/html" || held.MediaType == "application/json" {
		return Captured{Capture: held, Content: string(body)}, nil
	}
	return Captured{Capture: held, ContentBase64: base64.StdEncoding.EncodeToString(body)}, nil
}

// Bytes reads one capture only after checking the workspace/source row. The
// returned bytes are verified against the capture's immutable hash and size;
// derived readers use this same seam instead of reaching into blob storage by
// guessed content address.
func (s *Sources) Bytes(ctx context.Context, workspace, source, capture id.ID) (domain.Capture, []byte, error) {
	if workspace.IsZero() || source.IsZero() || capture.IsZero() {
		return domain.Capture{}, nil, domain.ErrInvalid
	}
	// The workspace/source predicate is checked before the content-addressed
	// store sees a hash; a guessed capture id never bypasses tenant isolation.
	// A purge preserves metadata and citations, but it revokes access to the
	// retained bytes even though the content-addressed store has no delete API.
	meta, err := s.reader.ByID(ctx, workspace, source)
	if err != nil {
		return domain.Capture{}, nil, err
	}
	if meta.PurgedAt != nil {
		return domain.Capture{}, nil, domain.ErrPurged
	}
	held, err := s.reader.Capture(ctx, workspace, source, capture)
	if err != nil {
		return domain.Capture{}, nil, err
	}
	ref, err := blob.ParseRef("sha256:" + held.SHA256)
	if err != nil {
		return domain.Capture{}, nil, err
	}
	reader, err := s.blobs.Open(ctx, ref)
	if err != nil {
		return domain.Capture{}, nil, err
	}
	defer reader.Close()
	body, err := io.ReadAll(io.LimitReader(reader, domain.MaxBinaryCaptureBytes+1))
	if err != nil {
		return domain.Capture{}, nil, err
	}
	sum := sha256.Sum256(body)
	if int64(len(body)) != held.Bytes || hex.EncodeToString(sum[:]) != held.SHA256 {
		return domain.Capture{}, nil, blob.ErrCorrupted
	}
	return held, body, nil
}
