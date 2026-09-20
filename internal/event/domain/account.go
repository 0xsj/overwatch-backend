package domain

import (
	"strings"
	"time"

	"github.com/0xsj/overwatch-backend/pkg/id"
)

type Account struct {
	ID                   id.ID         `json:"account_id"`
	WorkspaceID          id.ID         `json:"workspace_id"`
	EventID              id.ID         `json:"event_id"`
	Title                string        `json:"title"`
	Description          string        `json:"description,omitempty"`
	ReportedTime         string        `json:"reported_time,omitempty"`
	TimePrecision        TimePrecision `json:"time_precision"`
	SortDate             string        `json:"sort_date,omitempty"`
	Location             string        `json:"location,omitempty"`
	ObservationIDs       []id.ID       `json:"observation_ids"`
	ParticipantRecordIDs []id.ID       `json:"participant_record_ids"`
	LocationRecordID     *id.ID        `json:"location_record_id,omitempty"`
	Author               id.ID         `json:"author"`
	CreatedAt            time.Time     `json:"created_at"`
	UpdatedAt            time.Time     `json:"updated_at"`
}

type ReconciliationDecision string

const (
	DecisionUnresolved    ReconciliationDecision = "unresolved"
	DecisionRetainEvent   ReconciliationDecision = "retain_event"
	DecisionPreferAccount ReconciliationDecision = "prefer_account"
)

type Reconciliation struct {
	ID                id.ID                  `json:"reconciliation_id"`
	WorkspaceID       id.ID                  `json:"workspace_id"`
	EventID           id.ID                  `json:"event_id"`
	Decision          ReconciliationDecision `json:"decision"`
	SelectedAccountID *id.ID                 `json:"selected_account_id,omitempty"`
	Rationale         string                 `json:"rationale"`
	ReviewedBy        id.ID                  `json:"reviewed_by"`
	ReviewedAt        time.Time              `json:"reviewed_at"`
}

type AccountPage struct {
	Items          []Account       `json:"items"`
	Reconciliation *Reconciliation `json:"reconciliation,omitempty"`
}

func NewAccount(want, workspace, event, author id.ID, title, description, reportedTime, precision, sortDate, location string, observations, participants []id.ID, locationRecord *id.ID, at time.Time) (Account, error) {
	if want.IsZero() || workspace.IsZero() || event.IsZero() || author.IsZero() || at.IsZero() {
		return Account{}, ErrIDRequired
	}
	cleaned, err := clean(title, description, reportedTime, precision, sortDate, location, observations, participants, locationRecord)
	if err != nil {
		return Account{}, err
	}
	return Account{ID: want, WorkspaceID: workspace, EventID: event, Title: cleaned.title, Description: cleaned.description, ReportedTime: cleaned.reportedTime, TimePrecision: cleaned.precision, SortDate: cleaned.sortDate, Location: cleaned.location, ObservationIDs: cleaned.observations, ParticipantRecordIDs: cleaned.participants, LocationRecordID: cleaned.locationRecord, Author: author, CreatedAt: at, UpdatedAt: at}, nil
}

func NewReconciliation(want, workspace, event, reviewer id.ID, decision ReconciliationDecision, account *id.ID, rationale string, at time.Time) (Reconciliation, error) {
	if want.IsZero() || workspace.IsZero() || event.IsZero() || reviewer.IsZero() || at.IsZero() {
		return Reconciliation{}, ErrIDRequired
	}
	if decision != DecisionUnresolved && decision != DecisionRetainEvent && decision != DecisionPreferAccount {
		return Reconciliation{}, ErrReconciliationUnknown
	}
	rationale = strings.TrimSpace(rationale)
	if rationale == "" || !valid(rationale, MaxDescriptionLength) {
		return Reconciliation{}, ErrReconciliationRequired
	}
	if decision == DecisionPreferAccount && (account == nil || account.IsZero()) {
		return Reconciliation{}, ErrAccountRequired
	}
	if decision != DecisionPreferAccount && account != nil {
		return Reconciliation{}, ErrAccountRequired
	}
	var selected *id.ID
	if account != nil {
		value := *account
		selected = &value
	}
	return Reconciliation{ID: want, WorkspaceID: workspace, EventID: event, Decision: decision, SelectedAccountID: selected, Rationale: rationale, ReviewedBy: reviewer, ReviewedAt: at}, nil
}
