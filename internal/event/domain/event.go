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
)

type TimePrecision string

const (
	TimeUnknown     TimePrecision = "unknown"
	TimeExact       TimePrecision = "exact"
	TimeApproximate TimePrecision = "approximate"
	TimeRange       TimePrecision = "range"
)

func (p TimePrecision) String() string { return string(p) }

func ParseTimePrecision(raw string) (TimePrecision, error) {
	switch TimePrecision(strings.TrimSpace(raw)) {
	case TimeUnknown, TimeExact, TimeApproximate, TimeRange:
		return TimePrecision(strings.TrimSpace(raw)), nil
	default:
		return "", ErrTimeUnknown
	}
}

type Event struct {
	ID             id.ID         `json:"event_id"`
	WorkspaceID    id.ID         `json:"workspace_id"`
	Title          string        `json:"title"`
	Description    string        `json:"description,omitempty"`
	ReportedTime   string        `json:"reported_time,omitempty"`
	TimePrecision  TimePrecision `json:"time_precision"`
	SortDate       string        `json:"sort_date,omitempty"`
	Location       string        `json:"location,omitempty"`
	ObservationIDs []id.ID       `json:"observation_ids"`
	Author         id.ID         `json:"author"`
	UpdatedBy      id.ID         `json:"updated_by"`
	CreatedAt      time.Time     `json:"created_at"`
	UpdatedAt      time.Time     `json:"updated_at"`
}

func New(want, workspace, author id.ID, title, description, reportedTime, precision, sortDate, location string, observations []id.ID, at time.Time) (Event, error) {
	if want.IsZero() || author.IsZero() {
		return Event{}, ErrIDRequired
	}
	if workspace.IsZero() {
		return Event{}, ErrWorkspaceRequired
	}
	if at.IsZero() {
		return Event{}, ErrIDRequired
	}
	cleaned, err := clean(title, description, reportedTime, precision, sortDate, location, observations)
	if err != nil {
		return Event{}, err
	}
	return Event{ID: want, WorkspaceID: workspace, Title: cleaned.title, Description: cleaned.description, ReportedTime: cleaned.reportedTime, TimePrecision: cleaned.precision, SortDate: cleaned.sortDate, Location: cleaned.location, ObservationIDs: cleaned.observations, Author: author, UpdatedBy: author, CreatedAt: at, UpdatedAt: at}, nil
}

func (e Event) Edit(by id.ID, title, description, reportedTime, precision, sortDate, location string, observations []id.ID, at time.Time) (Event, error) {
	if by.IsZero() || at.IsZero() {
		return e, ErrIDRequired
	}
	cleaned, err := clean(title, description, reportedTime, precision, sortDate, location, observations)
	if err != nil {
		return e, err
	}
	next := e
	next.Title, next.Description = cleaned.title, cleaned.description
	next.ReportedTime, next.TimePrecision, next.SortDate = cleaned.reportedTime, cleaned.precision, cleaned.sortDate
	next.Location, next.ObservationIDs = cleaned.location, cleaned.observations
	next.UpdatedBy, next.UpdatedAt = by, at
	return next, nil
}

type cleanedEvent struct {
	title, description, reportedTime string
	precision                        TimePrecision
	sortDate, location               string
	observations                     []id.ID
}

func clean(title, description, reportedTime, precision, sortDate, location string, observations []id.ID) (cleanedEvent, error) {
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
	return cleanedEvent{title: title, description: description, reportedTime: reportedTime, precision: parsed, sortDate: sortDate, location: location, observations: links}, nil
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
