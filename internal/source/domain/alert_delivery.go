package domain

import (
	"sort"
	"strings"
	"time"

	"github.com/0xsj/overwatch-backend/pkg/errors"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

var ErrAlertDeliveryInvalid = errors.New(errors.Invalid, "source alert delivery settings are invalid")

// AlertDelivery is a per-account, per-workspace preference. It deliberately
// stores no address: identity remains the owner of account contact data, and
// this setting only records consent to send a notification there.
type AlertDelivery struct {
	WorkspaceID id.ID     `json:"workspace_id"`
	AccountID   id.ID     `json:"account_id"`
	Email       bool      `json:"email_enabled"`
	// An empty list means "all alert kinds" and is part of the response
	// contract. Omitting it makes a valid default deserialize as undefined in
	// clients that render the preference as a list.
	Kinds       []string  `json:"kinds"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func DefaultAlertDelivery(workspace, account id.ID) AlertDelivery {
	return AlertDelivery{WorkspaceID: workspace, AccountID: account, Kinds: []string{}}
}

func NewAlertDelivery(workspace, account id.ID, email bool, kinds []string, at time.Time) (AlertDelivery, error) {
	if workspace.IsZero() || account.IsZero() || at.IsZero() {
		return AlertDelivery{}, ErrAlertDeliveryInvalid
	}
	normalized := make([]string, 0, len(kinds))
	seen := make(map[string]struct{}, len(kinds))
	for _, raw := range kinds {
		kind := strings.TrimSpace(raw)
		if !validAlertKind(kind) {
			return AlertDelivery{}, ErrAlertDeliveryInvalid
		}
		if _, ok := seen[kind]; ok {
			continue
		}
		seen[kind] = struct{}{}
		normalized = append(normalized, kind)
	}
	sort.Strings(normalized)
	return AlertDelivery{WorkspaceID: workspace, AccountID: account, Email: email, Kinds: normalized, UpdatedAt: at}, nil
}

func (p AlertDelivery) Allows(kind string) bool {
	if !p.Email {
		return false
	}
	if len(p.Kinds) == 0 {
		return true
	}
	for _, allowed := range p.Kinds {
		if allowed == kind {
			return true
		}
	}
	return false
}

func validAlertKind(kind string) bool {
	switch kind {
	case AlertKindCaptureChanged, AlertKindWatchFailed, AlertKindQuestionGap, AlertKindRecordGap, AlertKindClusterGap:
		return true
	default:
		return false
	}
}
