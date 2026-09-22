package root

import (
	"context"
	"encoding/json"
	"fmt"

	identitydomain "github.com/0xsj/overwatch-backend/internal/identity/domain"
	sourcedomain "github.com/0xsj/overwatch-backend/internal/source/domain"
	"github.com/0xsj/overwatch-backend/pkg/events"
	owerrors "github.com/0xsj/overwatch-backend/pkg/errors"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

type alertDeliveryPreferences interface {
	AlertDelivery(context.Context, id.ID, id.ID) (sourcedomain.AlertDelivery, error)
}

type alertDeliveryAccounts interface {
	AccountByID(context.Context, id.ID) (identitydomain.Account, error)
}

type alertDeliveryMailer interface {
	Alert(context.Context, string, string, string) error
}

// alertDeliverySubscriber is deliberately a composition-root subscriber. The
// source domain owns alert preferences, identity owns addresses, and mail owns
// transport; only the root is allowed to join those three answers.
type alertDeliverySubscriber struct {
	preferences alertDeliveryPreferences
	accounts    alertDeliveryAccounts
	mailer      alertDeliveryMailer
}

func newAlertDeliverySubscriber(preferences alertDeliveryPreferences, accounts alertDeliveryAccounts, mailer alertDeliveryMailer) events.Handler {
	if preferences == nil || accounts == nil || mailer == nil {
		panic("root: newAlertDeliverySubscriber with a nil dependency")
	}
	subscriber := alertDeliverySubscriber{preferences: preferences, accounts: accounts, mailer: mailer}
	return subscriber.Handle
}

type alertCreatedPayload struct {
	WorkspaceID string `json:"workspace_id"`
	AccountID   string `json:"account_id"`
	Kind        string `json:"kind"`
	Title       string `json:"title"`
	Detail      string `json:"detail"`
}

func (s alertDeliverySubscriber) Handle(ctx context.Context, event events.Event) error {
	if event.Name != sourcedomain.EventAlertCreated {
		return nil
	}
	var payload alertCreatedPayload
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		return fmt.Errorf("alert delivery: decode event: %w", err)
	}
	workspace, err := id.Parse(payload.WorkspaceID)
	if err != nil {
		return fmt.Errorf("alert delivery: workspace: %w", err)
	}
	account, err := id.Parse(payload.AccountID)
	if err != nil {
		return fmt.Errorf("alert delivery: account: %w", err)
	}
	preference, err := s.preferences.AlertDelivery(ctx, workspace, account)
	if err != nil {
		return err
	}
	if !preference.Allows(payload.Kind) {
		return nil
	}
	person, err := s.accounts.AccountByID(ctx, account)
	if err != nil {
		// An archived account no longer has a deliverable address. The alert is
		// still retained in the inbox, so retrying this event cannot improve it.
		if owerrors.Is(err, identitydomain.ErrAccountNotFound) {
			return nil
		}
		return err
	}
	return s.mailer.Alert(ctx, person.Email.String(), payload.Title, payload.Detail)
}
