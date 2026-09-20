package command_test

import (
	"context"
	"testing"
	"time"

	"github.com/0xsj/overwatch-backend/internal/assistance/app/command"
	"github.com/0xsj/overwatch-backend/internal/assistance/domain"
	"github.com/0xsj/overwatch-backend/pkg/clock"
	"github.com/0xsj/overwatch-backend/pkg/events"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

type providerPolicyStore struct {
	policy domain.ProviderPolicy
	events []events.Event
}

func (s *providerPolicyStore) SaveProviderPolicy(_ context.Context, policy domain.ProviderPolicy) error {
	s.policy = policy
	return nil
}
func (s *providerPolicyStore) InTx(ctx context.Context, fn func(context.Context) error) error {
	return fn(ctx)
}
func (s *providerPolicyStore) Publish(_ context.Context, published ...events.Event) error {
	s.events = append(s.events, published...)
	return nil
}

func TestSettingProviderPolicyPersistsAnAuditableDecision(t *testing.T) {
	at := time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)
	ids := id.NewSequence(at)
	workspace, actor := ids.NewID(), ids.NewID()
	store := &providerPolicyStore{}
	service := command.NewProviderPolicies(store, store, store, ids, clock.NewFake(at))

	policy, err := service.Set(context.Background(), workspace, actor, true)
	if err != nil || !policy.AllowExternal || store.policy != policy {
		t.Fatalf("provider policy was not persisted: policy=%+v stored=%+v err=%v", policy, store.policy, err)
	}
	if len(store.events) != 1 || store.events[0].Name != domain.EventProviderPolicyUpdated || !store.events[0].Decision {
		t.Fatalf("provider policy decision was not audited: %+v", store.events)
	}
}
