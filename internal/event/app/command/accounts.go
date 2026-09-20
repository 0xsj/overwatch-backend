package command

import (
	"context"

	"github.com/0xsj/overwatch-backend/internal/event/domain"
	recorddomain "github.com/0xsj/overwatch-backend/internal/researchentity/domain"
	"github.com/0xsj/overwatch-backend/pkg/events"
	"github.com/0xsj/overwatch-backend/pkg/id"
	"github.com/0xsj/overwatch-backend/pkg/provenance"
)

type AccountRepository interface {
	CreateAccount(context.Context, domain.Account) error
	AccountByID(context.Context, id.ID, id.ID, id.ID) (domain.Account, error)
	CreateReconciliation(context.Context, domain.Reconciliation) error
}

type EventReader interface {
	ByID(context.Context, id.ID, id.ID) (domain.Event, error)
}

type AccountRecords interface {
	ByID(context.Context, id.ID, id.ID) (recorddomain.Record, error)
}

type Accounts struct {
	repo      AccountRepository
	events    EventReader
	records   AccountRecords
	tx        Transactor
	publisher events.Publisher
	ids       Minter
	clock     Clock
}

func NewAccounts(repo AccountRepository, eventReader EventReader, records AccountRecords, tx Transactor, publisher events.Publisher, ids Minter, clock Clock) *Accounts {
	if repo == nil || eventReader == nil || records == nil || tx == nil || publisher == nil || ids == nil || clock == nil {
		panic("event: NewAccounts with a nil dependency")
	}
	return &Accounts{repo: repo, events: eventReader, records: records, tx: tx, publisher: publisher, ids: ids, clock: clock}
}

func (a *Accounts) Create(ctx context.Context, workspace, event, author id.ID, title, description, reportedTime, precision, sortDate, location string, observations, participants []id.ID, locationRecord *id.ID) (domain.Account, error) {
	if _, err := a.events.ByID(ctx, workspace, event); err != nil {
		return domain.Account{}, err
	}
	fresh, err := domain.NewAccount(a.ids.NewID(), workspace, event, author, title, description, reportedTime, precision, sortDate, location, observations, participants, locationRecord, a.clock.Now())
	if err != nil {
		return domain.Account{}, err
	}
	if err := a.validateRecords(ctx, workspace, fresh.ParticipantRecordIDs, fresh.LocationRecordID); err != nil {
		return domain.Account{}, err
	}
	if err := a.tx.InTx(ctx, func(ctx context.Context) error {
		if err := a.repo.CreateAccount(ctx, fresh); err != nil {
			return err
		}
		return a.publish(ctx, workspace, fresh, "created")
	}); err != nil {
		return domain.Account{}, err
	}
	return fresh, nil
}

func (a *Accounts) Reconcile(ctx context.Context, workspace, event, reviewer id.ID, decision domain.ReconciliationDecision, account *id.ID, rationale string) (domain.Reconciliation, error) {
	if _, err := a.events.ByID(ctx, workspace, event); err != nil {
		return domain.Reconciliation{}, err
	}
	if account != nil {
		if _, err := a.repo.AccountByID(ctx, workspace, event, *account); err != nil {
			return domain.Reconciliation{}, err
		}
	}
	fresh, err := domain.NewReconciliation(a.ids.NewID(), workspace, event, reviewer, decision, account, rationale, a.clock.Now())
	if err != nil {
		return domain.Reconciliation{}, err
	}
	if err := a.tx.InTx(ctx, func(ctx context.Context) error {
		if err := a.repo.CreateReconciliation(ctx, fresh); err != nil {
			return err
		}
		return a.publish(ctx, workspace, fresh, "reconciled")
	}); err != nil {
		return domain.Reconciliation{}, err
	}
	return fresh, nil
}

func (a *Accounts) validateRecords(ctx context.Context, workspace id.ID, participants []id.ID, location *id.ID) error {
	for _, record := range participants {
		if _, err := a.records.ByID(ctx, workspace, record); err != nil {
			return err
		}
	}
	if location != nil {
		record, err := a.records.ByID(ctx, workspace, *location)
		if err != nil {
			return err
		}
		if record.Kind != recorddomain.Place {
			return domain.ErrLocationRecordKind
		}
	}
	return nil
}

func (a *Accounts) publish(ctx context.Context, workspace id.ID, subject any, change string) error {
	prov, ok := provenance.Current(ctx)
	if !ok {
		prov = provenance.New(provenance.OriginRequest, a.ids)
	}
	prov, err := prov.WithTenant(workspace.String())
	if err != nil {
		return err
	}
	var eventID id.ID
	switch value := subject.(type) {
	case domain.Account:
		eventID = value.EventID
	case domain.Reconciliation:
		eventID = value.EventID
	}
	decision, err := events.NewDecision(a.ids, a.clock, domain.EventChanged, "workspace:"+workspace.String(), prov, map[string]any{
		"workspace_id": workspace.String(), "event_id": eventID.String(), "account_change": change,
	})
	if err != nil {
		return err
	}
	return a.publisher.Publish(ctx, decision)
}
