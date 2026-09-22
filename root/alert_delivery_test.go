package root

import (
	"context"
	"encoding/json"
	"testing"

	identitydomain "github.com/0xsj/overwatch-backend/internal/identity/domain"
	sourcedomain "github.com/0xsj/overwatch-backend/internal/source/domain"
	"github.com/0xsj/overwatch-backend/pkg/events"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

type alertDeliveryPreferenceStub struct {
	preference sourcedomain.AlertDelivery
}

func (s alertDeliveryPreferenceStub) AlertDelivery(context.Context, id.ID, id.ID) (sourcedomain.AlertDelivery, error) {
	return s.preference, nil
}

type alertDeliveryAccountStub struct {
	account identitydomain.Account
}

func (s alertDeliveryAccountStub) AccountByID(context.Context, id.ID) (identitydomain.Account, error) {
	return s.account, nil
}

type alertDeliveryMailerStub struct {
	to, title, detail string
}

func (s *alertDeliveryMailerStub) Alert(_ context.Context, to, title, detail string) error {
	s.to, s.title, s.detail = to, title, detail
	return nil
}

func TestAlertDeliverySubscriberRequiresPreferenceAndDeliversAllowedAlert(t *testing.T) {
	workspace, accountID := id.ID{1}, id.ID{2}
	payload, err := json.Marshal(alertCreatedPayload{
		WorkspaceID: workspace.String(), AccountID: accountID.String(),
		Kind: "capture_changed", Title: "Source changed", Detail: "A new capture was retained.",
	})
	if err != nil {
		t.Fatal(err)
	}
	mailer := new(alertDeliveryMailerStub)
	handler := newAlertDeliverySubscriber(
		alertDeliveryPreferenceStub{preference: sourcedomain.AlertDelivery{WorkspaceID: workspace, AccountID: accountID, Email: true}},
		alertDeliveryAccountStub{account: identitydomain.Account{ID: accountID, Email: identitydomain.Email("analyst@example.com")}},
		mailer,
	)
	if err := handler(context.Background(), events.Event{Name: sourcedomain.EventAlertCreated, Payload: payload}); err != nil {
		t.Fatal(err)
	}
	if mailer.to != "analyst@example.com" || mailer.title != "Source changed" || mailer.detail == "" {
		t.Fatalf("unexpected delivered alert: %+v", mailer)
	}
}

func TestAlertDeliverySubscriberSkipsDisabledPreference(t *testing.T) {
	mailer := new(alertDeliveryMailerStub)
	handler := newAlertDeliverySubscriber(
		alertDeliveryPreferenceStub{preference: sourcedomain.AlertDelivery{Email: false}},
		alertDeliveryAccountStub{},
		mailer,
	)
	payload, err := json.Marshal(alertCreatedPayload{WorkspaceID: id.ID{1}.String(), AccountID: id.ID{2}.String(), Kind: "capture_changed"})
	if err != nil {
		t.Fatal(err)
	}
	if err := handler(context.Background(), events.Event{Name: sourcedomain.EventAlertCreated, Payload: payload}); err != nil {
		t.Fatal(err)
	}
	if mailer.to != "" {
		t.Fatalf("disabled preference sent mail: %+v", mailer)
	}
}
