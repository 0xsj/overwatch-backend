package domain

import (
	"bytes"
	"math"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/0xsj/overwatch-backend/pkg/id"
)

const (
	MaxNameLength         = 400
	MaxDescriptionLength  = 4000
	MaxObservationLinks   = 12
	MaxPlaceGeometryLinks = 8
)

type Kind string

const (
	Person       Kind = "person"
	Account      Kind = "account"
	Organisation Kind = "organisation"
	Place        Kind = "place"
)

func (k Kind) String() string { return string(k) }

func ParseKind(raw string) (Kind, error) {
	switch Kind(strings.TrimSpace(raw)) {
	case Person, Account, Organisation, Place:
		return Kind(strings.TrimSpace(raw)), nil
	default:
		return "", ErrKindUnknown
	}
}

type CitationFilter string

const (
	CitationAny     CitationFilter = ""
	CitationCited   CitationFilter = "cited"
	CitationUncited CitationFilter = "uncited"
)

func (f CitationFilter) String() string { return string(f) }

func ParseCitationFilter(raw string) (CitationFilter, error) {
	switch CitationFilter(strings.TrimSpace(raw)) {
	case CitationAny, CitationCited, CitationUncited:
		return CitationFilter(strings.TrimSpace(raw)), nil
	default:
		return "", ErrCitationFilterUnknown
	}
}

type ResolutionFilter string

const (
	ResolutionAny      ResolutionFilter = ""
	ResolutionOpen     ResolutionFilter = "open"
	ResolutionAccepted ResolutionFilter = "accepted"
	ResolutionNone     ResolutionFilter = "none"
)

func (f ResolutionFilter) String() string { return string(f) }

type ArchiveFilter string

const (
	ArchiveActive   ArchiveFilter = "active"
	ArchiveArchived ArchiveFilter = "archived"
	ArchiveAll      ArchiveFilter = "all"
)

func (f ArchiveFilter) String() string { return string(f) }

func ParseArchiveFilter(raw string) (ArchiveFilter, error) {
	switch ArchiveFilter(strings.TrimSpace(raw)) {
	case ArchiveActive, ArchiveArchived, ArchiveAll:
		return ArchiveFilter(strings.TrimSpace(raw)), nil
	default:
		return "", ErrArchiveFilterUnknown
	}
}

// PlacePrecision qualifies how much the cited material supports a point. A
// region point is a representative point, not a boundary or an exact address.
type PlacePrecision string

const (
	PlaceExact       PlacePrecision = "exact"
	PlaceApproximate PlacePrecision = "approximate"
	PlaceRegion      PlacePrecision = "region"
)

func (p PlacePrecision) String() string { return string(p) }

func ParsePlacePrecision(raw string) (PlacePrecision, error) {
	switch PlacePrecision(strings.TrimSpace(raw)) {
	case PlaceExact, PlaceApproximate, PlaceRegion:
		return PlacePrecision(strings.TrimSpace(raw)), nil
	default:
		return "", ErrPlacePrecisionUnknown
	}
}

// PlaceGeometry is an investigator-authored point backed by cited
// observations. Overwatch never geocodes a name into this structure.
type PlaceGeometry struct {
	Latitude       float64        `json:"latitude"`
	Longitude      float64        `json:"longitude"`
	Precision      PlacePrecision `json:"precision"`
	ObservationIDs []id.ID        `json:"observation_ids"`
}

func ParseResolutionFilter(raw string) (ResolutionFilter, error) {
	switch ResolutionFilter(strings.TrimSpace(raw)) {
	case ResolutionAny, ResolutionOpen, ResolutionAccepted, ResolutionNone:
		return ResolutionFilter(strings.TrimSpace(raw)), nil
	default:
		return "", ErrResolutionFilterUnknown
	}
}

// Record is a researcher-authored OSINT object. It names what the researcher
// is tracking without asserting that two records identify the same real-world
// subject.
type Record struct {
	ID             id.ID          `json:"record_id"`
	WorkspaceID    id.ID          `json:"workspace_id"`
	Kind           Kind           `json:"kind"`
	Name           string         `json:"name"`
	Description    string         `json:"description,omitempty"`
	ObservationIDs []id.ID        `json:"observation_ids"`
	PlaceGeometry  *PlaceGeometry `json:"place_geometry,omitempty"`
	Author         id.ID          `json:"author"`
	UpdatedBy      id.ID          `json:"updated_by"`
	CreatedAt      time.Time      `json:"created_at"`
	UpdatedAt      time.Time      `json:"updated_at"`
	ArchivedAt     time.Time      `json:"archived_at,omitempty"`
	ArchivedBy     id.ID          `json:"archived_by,omitempty"`
}

func (r Record) Archived() bool { return !r.ArchivedAt.IsZero() }

// Revision is an append-only snapshot of an authored record. It preserves the
// record fields and cited observations as they were when the change occurred,
// so later readers do not have to reconstruct history from the live row.
type Revision struct {
	ID             id.ID
	WorkspaceID    id.ID
	RecordID       id.ID
	Revision       int
	Kind           Kind
	Name           string
	Description    string
	ObservationIDs []id.ID
	PlaceGeometry  *PlaceGeometry
	ArchivedAt     time.Time
	ArchivedBy     id.ID
	ChangedBy      id.ID
	ChangedAt      time.Time
}

// NewRevision snapshots a record for an append-only history store.
func NewRevision(want id.ID, record Record) Revision {
	var geometry *PlaceGeometry
	if record.PlaceGeometry != nil {
		geometry = &PlaceGeometry{
			Latitude: record.PlaceGeometry.Latitude, Longitude: record.PlaceGeometry.Longitude,
			Precision:      record.PlaceGeometry.Precision,
			ObservationIDs: append([]id.ID(nil), record.PlaceGeometry.ObservationIDs...),
		}
	}
	return Revision{
		ID: want, WorkspaceID: record.WorkspaceID, RecordID: record.ID,
		Kind: record.Kind, Name: record.Name, Description: record.Description,
		ObservationIDs: append([]id.ID(nil), record.ObservationIDs...), PlaceGeometry: geometry,
		ArchivedAt: record.ArchivedAt, ArchivedBy: record.ArchivedBy,
		ChangedBy: record.UpdatedBy, ChangedAt: record.UpdatedAt,
	}
}

// BrowseSummary is a workspace-scoped read model for the authored record
// estate. It describes coverage and review context without changing any
// record or resolution state.
type BrowseSummary struct {
	RecordCount                   int
	KindCounts                    map[Kind]int
	CitedRecordCount              int
	UncitedRecordCount            int
	CitationCount                 int
	OpenResolutionRecordCount     int
	AcceptedResolutionRecordCount int
}

func New(want, workspace, author id.ID, kind, name, description string, observations []id.ID, at time.Time) (Record, error) {
	return NewWithPlaceGeometry(want, workspace, author, kind, name, description, observations, nil, at)
}

func NewWithPlaceGeometry(want, workspace, author id.ID, kind, name, description string, observations []id.ID, geometry *PlaceGeometry, at time.Time) (Record, error) {
	if want.IsZero() || author.IsZero() {
		return Record{}, ErrIDRequired
	}
	if workspace.IsZero() {
		return Record{}, ErrWorkspaceRequired
	}
	if at.IsZero() {
		return Record{}, ErrTimeRequired
	}
	cleaned, err := clean(kind, name, description, observations, geometry)
	if err != nil {
		return Record{}, err
	}
	return Record{ID: want, WorkspaceID: workspace, Kind: cleaned.kind, Name: cleaned.name,
		Description: cleaned.description, ObservationIDs: cleaned.observations, PlaceGeometry: cleaned.geometry,
		Author: author, UpdatedBy: author, CreatedAt: at, UpdatedAt: at}, nil
}

func (r Record) Edit(by id.ID, kind, name, description string, observations []id.ID, at time.Time) (Record, error) {
	return r.EditWithPlaceGeometry(by, kind, name, description, observations, r.PlaceGeometry, at)
}

func (r Record) EditWithPlaceGeometry(by id.ID, kind, name, description string, observations []id.ID, geometry *PlaceGeometry, at time.Time) (Record, error) {
	if r.Archived() {
		return r, ErrArchived
	}
	if by.IsZero() {
		return r, ErrIDRequired
	}
	if at.IsZero() {
		return r, ErrTimeRequired
	}
	cleaned, err := clean(kind, name, description, observations, geometry)
	if err != nil {
		return r, err
	}
	next := r
	next.Kind, next.Name, next.Description = cleaned.kind, cleaned.name, cleaned.description
	next.ObservationIDs, next.PlaceGeometry = cleaned.observations, cleaned.geometry
	next.UpdatedBy, next.UpdatedAt = by, at
	return next, nil
}

func (r Record) Archive(by id.ID, at time.Time) (Record, error) {
	if by.IsZero() {
		return r, ErrIDRequired
	}
	if at.IsZero() {
		return r, ErrTimeRequired
	}
	next := r
	next.ArchivedAt, next.ArchivedBy = at, by
	next.UpdatedBy, next.UpdatedAt = by, at
	return next, nil
}

func (r Record) Restore(by id.ID, at time.Time) (Record, error) {
	if by.IsZero() {
		return r, ErrIDRequired
	}
	if at.IsZero() {
		return r, ErrTimeRequired
	}
	next := r
	next.ArchivedAt, next.ArchivedBy = time.Time{}, id.ID{}
	next.UpdatedBy, next.UpdatedAt = by, at
	return next, nil
}

type cleanedRecord struct {
	kind              Kind
	name, description string
	observations      []id.ID
	geometry          *PlaceGeometry
}

func clean(kind, name, description string, observations []id.ID, geometry *PlaceGeometry) (cleanedRecord, error) {
	parsed, err := ParseKind(kind)
	if err != nil {
		return cleanedRecord{}, err
	}
	name, description = strings.TrimSpace(name), strings.TrimSpace(description)
	if name == "" || !valid(name, MaxNameLength) {
		if name != "" && len(name) > MaxNameLength {
			return cleanedRecord{}, ErrNameTooLong
		}
		return cleanedRecord{}, ErrNameRequired
	}
	if !valid(description, MaxDescriptionLength) {
		return cleanedRecord{}, ErrDescriptionTooLong
	}
	links, err := observationLinks(observations)
	if err != nil {
		return cleanedRecord{}, err
	}
	cleanedGeometry, err := cleanPlaceGeometry(parsed, geometry, links)
	if err != nil {
		return cleanedRecord{}, err
	}
	return cleanedRecord{kind: parsed, name: name, description: description, observations: links, geometry: cleanedGeometry}, nil
}

func cleanPlaceGeometry(kind Kind, input *PlaceGeometry, recordObservations []id.ID) (*PlaceGeometry, error) {
	if input == nil {
		return nil, nil
	}
	if kind != Place {
		return nil, ErrPlaceGeometryKind
	}
	if math.IsNaN(input.Latitude) || math.IsInf(input.Latitude, 0) || input.Latitude < -90 || input.Latitude > 90 ||
		math.IsNaN(input.Longitude) || math.IsInf(input.Longitude, 0) || input.Longitude < -180 || input.Longitude > 180 {
		return nil, ErrPlaceGeometryCoordinates
	}
	precision, err := ParsePlacePrecision(input.Precision.String())
	if err != nil {
		return nil, err
	}
	if len(input.ObservationIDs) == 0 || len(input.ObservationIDs) > MaxPlaceGeometryLinks {
		return nil, ErrPlaceGeometryEvidence
	}
	links, err := observationLinks(input.ObservationIDs)
	if err != nil {
		return nil, err
	}
	known := make(map[id.ID]struct{}, len(recordObservations))
	for _, observation := range recordObservations {
		known[observation] = struct{}{}
	}
	for _, observation := range links {
		if _, ok := known[observation]; !ok {
			return nil, ErrPlaceGeometryEvidence
		}
	}
	return &PlaceGeometry{Latitude: input.Latitude, Longitude: input.Longitude, Precision: precision, ObservationIDs: links}, nil
}

func valid(value string, limit int) bool {
	return utf8.ValidString(value) && !strings.ContainsRune(value, 0) && len(value) <= limit
}

func observationLinks(input []id.ID) ([]id.ID, error) {
	if len(input) > MaxObservationLinks {
		return nil, ErrObservationTooMany
	}
	links := append([]id.ID(nil), input...)
	sort.Slice(links, func(i, j int) bool { return bytes.Compare(links[i][:], links[j][:]) < 0 })
	for i, one := range links {
		if one.IsZero() {
			return nil, ErrIDRequired
		}
		if i > 0 && links[i-1] == one {
			return nil, ErrDuplicateObservation
		}
	}
	return links, nil
}

const EventChanged = "research.entity.changed"

type Changed struct {
	WorkspaceID string `json:"workspace_id"`
	RecordID    string `json:"record_id"`
	UpdatedBy   string `json:"updated_by"`
	Edit        bool   `json:"edit"`
	Archive     bool   `json:"archive"`
	Restore     bool   `json:"restore"`
}
