package domain

import (
	"bytes"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/0xsj/overwatch-backend/pkg/id"
)

const (
	EventChanged         = "timeline.event.changed"
	MaxTitleLength       = 400
	MaxDescriptionLength = 4000
	MaxTimeTextLength    = 200
	MaxLocationLength    = 400
	MaxObservationLinks  = 8
	MaxParticipantLinks  = 8
)

type TimePrecision string

const (
	TimeUnknown     TimePrecision = "unknown"
	TimeExact       TimePrecision = "exact"
	TimeApproximate TimePrecision = "approximate"
	TimeRange       TimePrecision = "range"
)

func (p TimePrecision) String() string { return string(p) }

type ParticipantRole string

const (
	ParticipantAssociated ParticipantRole = "associated"
	ParticipantActor      ParticipantRole = "actor"
	ParticipantSubject    ParticipantRole = "subject"
	ParticipantTarget     ParticipantRole = "target"
	ParticipantWitness    ParticipantRole = "witness"
	ParticipantAffected   ParticipantRole = "affected"
	ParticipantReporter   ParticipantRole = "reporter"
)

func (r ParticipantRole) String() string { return string(r) }

type ParticipantLink struct {
	RecordID id.ID           `json:"record_id"`
	Role     ParticipantRole `json:"role"`
}

type PlaceGeometrySnapshot struct {
	Latitude       float64 `json:"latitude"`
	Longitude      float64 `json:"longitude"`
	Precision      string  `json:"precision"`
	ObservationIDs []id.ID `json:"observation_ids"`
}

func ParseTimePrecision(raw string) (TimePrecision, error) {
	switch TimePrecision(strings.TrimSpace(raw)) {
	case TimeUnknown, TimeExact, TimeApproximate, TimeRange:
		return TimePrecision(strings.TrimSpace(raw)), nil
	default:
		return "", ErrTimeUnknown
	}
}

type Event struct {
	ID                   id.ID             `json:"event_id"`
	WorkspaceID          id.ID             `json:"workspace_id"`
	Title                string            `json:"title"`
	Description          string            `json:"description,omitempty"`
	ReportedTime         string            `json:"reported_time,omitempty"`
	TimePrecision        TimePrecision     `json:"time_precision"`
	SortDate             string            `json:"sort_date,omitempty"`
	Location             string            `json:"location,omitempty"`
	ObservationIDs       []id.ID           `json:"observation_ids"`
	ParticipantRecordIDs []id.ID           `json:"participant_record_ids"`
	ParticipantLinks     []ParticipantLink `json:"participant_links"`
	LocationRecordID     *id.ID            `json:"location_record_id,omitempty"`
	Author               id.ID             `json:"author"`
	UpdatedBy            id.ID             `json:"updated_by"`
	CreatedAt            time.Time         `json:"created_at"`
	UpdatedAt            time.Time         `json:"updated_at"`
}

// RecordSnapshot is the authored record context that was visible when an
// event revision was written. Revisions must not resolve mutable records at
// read time because a later edit would rewrite the historical account.
type RecordSnapshot struct {
	ID             id.ID                  `json:"record_id"`
	Kind           string                 `json:"kind"`
	Name           string                 `json:"name"`
	Description    string                 `json:"description,omitempty"`
	ObservationIDs []id.ID                `json:"observation_ids"`
	PlaceGeometry  *PlaceGeometrySnapshot `json:"place_geometry,omitempty"`
	Role           ParticipantRole        `json:"role,omitempty"`
}

// Revision is an append-only copy of a reported event. It keeps the event's
// wording, time qualification, citations, and linked record context together
// so a later reader can explain what changed without reading the live row.
type Revision struct {
	ID                   id.ID             `json:"revision_id"`
	WorkspaceID          id.ID             `json:"workspace_id"`
	EventID              id.ID             `json:"event_id"`
	Revision             int               `json:"revision"`
	Title                string            `json:"title"`
	Description          string            `json:"description,omitempty"`
	ReportedTime         string            `json:"reported_time,omitempty"`
	TimePrecision        TimePrecision     `json:"time_precision"`
	SortDate             string            `json:"sort_date,omitempty"`
	Location             string            `json:"location,omitempty"`
	ObservationIDs       []id.ID           `json:"observation_ids"`
	ParticipantRecordIDs []id.ID           `json:"participant_record_ids"`
	ParticipantLinks     []ParticipantLink `json:"participant_links"`
	ParticipantRecords   []RecordSnapshot  `json:"participant_records"`
	LocationRecordID     *id.ID            `json:"location_record_id,omitempty"`
	LocationRecord       *RecordSnapshot   `json:"location_record,omitempty"`
	ChangedBy            id.ID             `json:"changed_by"`
	ChangedAt            time.Time         `json:"changed_at"`
}

func New(want, workspace, author id.ID, title, description, reportedTime, precision, sortDate, location string, observations []id.ID, at time.Time) (Event, error) {
	return NewWithLinks(want, workspace, author, title, description, reportedTime, precision, sortDate, location, observations, nil, nil, at)
}

func NewWithLinks(want, workspace, author id.ID, title, description, reportedTime, precision, sortDate, location string, observations, participants []id.ID, locationRecord *id.ID, at time.Time) (Event, error) {
	return NewWithParticipantLinks(want, workspace, author, title, description, reportedTime, precision, sortDate, location, observations, participantLinksFromIDs(participants), locationRecord, at)
}

func NewWithParticipantLinks(want, workspace, author id.ID, title, description, reportedTime, precision, sortDate, location string, observations []id.ID, participants []ParticipantLink, locationRecord *id.ID, at time.Time) (Event, error) {
	if want.IsZero() || author.IsZero() {
		return Event{}, ErrIDRequired
	}
	if workspace.IsZero() {
		return Event{}, ErrWorkspaceRequired
	}
	if at.IsZero() {
		return Event{}, ErrIDRequired
	}
	cleaned, err := cleanWithParticipantLinks(title, description, reportedTime, precision, sortDate, location, observations, participants, locationRecord)
	if err != nil {
		return Event{}, err
	}
	return Event{ID: want, WorkspaceID: workspace, Title: cleaned.title, Description: cleaned.description, ReportedTime: cleaned.reportedTime, TimePrecision: cleaned.precision, SortDate: cleaned.sortDate, Location: cleaned.location, ObservationIDs: cleaned.observations, ParticipantRecordIDs: participantIDsFromLinks(cleaned.participantLinks), ParticipantLinks: cleaned.participantLinks, LocationRecordID: cleaned.locationRecord, Author: author, UpdatedBy: author, CreatedAt: at, UpdatedAt: at}, nil
}

func (e Event) Edit(by id.ID, title, description, reportedTime, precision, sortDate, location string, observations []id.ID, at time.Time) (Event, error) {
	participants := e.ParticipantLinks
	if len(participants) == 0 && len(e.ParticipantRecordIDs) > 0 {
		participants = participantLinksFromIDs(e.ParticipantRecordIDs)
	}
	return e.EditWithParticipantLinks(by, title, description, reportedTime, precision, sortDate, location, observations, participants, e.LocationRecordID, at)
}

func (e Event) EditWithLinks(by id.ID, title, description, reportedTime, precision, sortDate, location string, observations, participants []id.ID, locationRecord *id.ID, at time.Time) (Event, error) {
	return e.EditWithParticipantLinks(by, title, description, reportedTime, precision, sortDate, location, observations, participantLinksFromIDs(participants), locationRecord, at)
}

func (e Event) EditWithParticipantLinks(by id.ID, title, description, reportedTime, precision, sortDate, location string, observations []id.ID, participants []ParticipantLink, locationRecord *id.ID, at time.Time) (Event, error) {
	if by.IsZero() || at.IsZero() {
		return e, ErrIDRequired
	}
	cleaned, err := cleanWithParticipantLinks(title, description, reportedTime, precision, sortDate, location, observations, participants, locationRecord)
	if err != nil {
		return e, err
	}
	next := e
	next.Title, next.Description = cleaned.title, cleaned.description
	next.ReportedTime, next.TimePrecision, next.SortDate = cleaned.reportedTime, cleaned.precision, cleaned.sortDate
	next.Location, next.ObservationIDs = cleaned.location, cleaned.observations
	next.ParticipantRecordIDs, next.ParticipantLinks, next.LocationRecordID = participantIDsFromLinks(cleaned.participantLinks), cleaned.participantLinks, cleaned.locationRecord
	next.UpdatedBy, next.UpdatedAt = by, at
	return next, nil
}

type cleanedEvent struct {
	title, description, reportedTime string
	precision                        TimePrecision
	sortDate, location               string
	observations                     []id.ID
	participants                     []id.ID
	participantLinks                 []ParticipantLink
	locationRecord                   *id.ID
}

func clean(title, description, reportedTime, precision, sortDate, location string, observations, participants []id.ID, locationRecord *id.ID) (cleanedEvent, error) {
	return cleanWithParticipantLinks(title, description, reportedTime, precision, sortDate, location, observations, participantLinksFromIDs(participants), locationRecord)
}

func cleanWithParticipantLinks(title, description, reportedTime, precision, sortDate, location string, observations []id.ID, participants []ParticipantLink, locationRecord *id.ID) (cleanedEvent, error) {
	title, description = strings.TrimSpace(title), strings.TrimSpace(description)
	reportedTime, sortDate, location = strings.TrimSpace(reportedTime), strings.TrimSpace(sortDate), strings.TrimSpace(location)
	if title == "" || !valid(title, MaxTitleLength) {
		return cleanedEvent{}, ErrTitleRequired
	}
	if !valid(description, MaxDescriptionLength) {
		return cleanedEvent{}, ErrDescriptionTooLong
	}
	if !valid(reportedTime, MaxTimeTextLength) {
		return cleanedEvent{}, ErrTimeTextTooLong
	}
	if !valid(location, MaxLocationLength) {
		return cleanedEvent{}, ErrLocationTooLong
	}
	parsed, err := ParseTimePrecision(precision)
	if err != nil {
		return cleanedEvent{}, err
	}
	if parsed == TimeUnknown && (reportedTime != "" || sortDate != "") {
		return cleanedEvent{}, ErrTimeRequired
	}
	if parsed != TimeUnknown && reportedTime == "" {
		return cleanedEvent{}, ErrTimeRequired
	}
	if sortDate != "" {
		if len(sortDate) != len("2006-01-02") {
			return cleanedEvent{}, ErrSortDateInvalid
		}
		if _, err := time.Parse("2006-01-02", sortDate); err != nil {
			return cleanedEvent{}, ErrSortDateInvalid
		}
	}
	links, err := observationLinks(observations)
	if err != nil {
		return cleanedEvent{}, err
	}
	participantLinks, err := participantLinksClean(participants)
	if err != nil {
		return cleanedEvent{}, err
	}
	var locationLink *id.ID
	if locationRecord != nil {
		if locationRecord.IsZero() {
			return cleanedEvent{}, ErrIDRequired
		}
		value := *locationRecord
		locationLink = &value
	}
	return cleanedEvent{title: title, description: description, reportedTime: reportedTime, precision: parsed, sortDate: sortDate, location: location, observations: links, participants: participantIDsFromLinks(participantLinks), participantLinks: participantLinks, locationRecord: locationLink}, nil
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

func participantLinksFromIDs(input []id.ID) []ParticipantLink {
	links := make([]ParticipantLink, 0, len(input))
	for _, recordID := range input {
		links = append(links, ParticipantLink{RecordID: recordID, Role: ParticipantAssociated})
	}
	return links
}

func participantIDsFromLinks(input []ParticipantLink) []id.ID {
	ids := make([]id.ID, 0, len(input))
	for _, link := range input {
		ids = append(ids, link.RecordID)
	}
	return ids
}

func participantLinksClean(input []ParticipantLink) ([]ParticipantLink, error) {
	if len(input) > MaxParticipantLinks {
		return nil, ErrParticipantTooMany
	}
	links := append([]ParticipantLink(nil), input...)
	sort.Slice(links, func(i, j int) bool { return bytes.Compare(links[i].RecordID[:], links[j].RecordID[:]) < 0 })
	for i, one := range links {
		if one.RecordID.IsZero() {
			return nil, ErrIDRequired
		}
		if !validParticipantRole(one.Role) {
			return nil, ErrParticipantRoleUnknown
		}
		if i > 0 && links[i-1].RecordID == one.RecordID {
			return nil, ErrDuplicateParticipant
		}
	}
	return links, nil
}

func validParticipantRole(role ParticipantRole) bool {
	switch role {
	case ParticipantAssociated, ParticipantActor, ParticipantSubject, ParticipantTarget, ParticipantWitness, ParticipantAffected, ParticipantReporter:
		return true
	default:
		return false
	}
}
