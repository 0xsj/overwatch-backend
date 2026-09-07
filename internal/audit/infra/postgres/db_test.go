package postgres_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	auditapp "github.com/0xsj/overwatch-backend/internal/audit/app"
	"github.com/0xsj/overwatch-backend/internal/audit/domain"
	auditpg "github.com/0xsj/overwatch-backend/internal/audit/infra/postgres"
	"github.com/0xsj/overwatch-backend/pkg/clock"
	"github.com/0xsj/overwatch-backend/pkg/events"
	"github.com/0xsj/overwatch-backend/pkg/id"
	"github.com/0xsj/overwatch-backend/pkg/postgres"
	"github.com/0xsj/overwatch-backend/pkg/provenance"
	"github.com/0xsj/overwatch-backend/pkg/testx"
)

var at = time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)

type fixedClock struct{}

func (fixedClock) Now() time.Time { return at }

func store(t *testing.T) (*auditpg.Store, *postgres.Pool) {
	t.Helper()
	p := testx.Postgres(t, testx.Schema{Name: auditpg.Schema, Migrations: auditpg.Migrations})
	return auditpg.NewStore(p), p
}

func ids() *id.Sequence { return id.NewSequence(at) }

func decision(t *testing.T, m *id.Sequence, name, subject string, p provenance.Provenance, payload any) events.Event {
	t.Helper()
	e, err := events.NewDecision(m, fixedClock{}, name, subject, p, payload)
	if err != nil {
		t.Fatalf("mint %s: %v", name, err)
	}
	return e
}

func actor(t *testing.T, m *id.Sequence, who string) provenance.Provenance {
	t.Helper()
	a, err := provenance.User(who)
	if err != nil {
		t.Fatal(err)
	}
	return provenance.New(provenance.OriginRequest, m).WithActor(a)
}

func TestOnlyADecisionEarnsAnEntry(t *testing.T) {
	s, _ := store(t)
	ctx := context.Background()
	m := ids()
	sub := auditapp.NewSubscriber(s, m, clock.System{})
	p := actor(t, m, "acct_sj")

	// A person STARTING a run is work, not a decision — the shortcut this rule
	// guards against is "the actor is a person, therefore audit".
	work, err := events.New(m, fixedClock{}, "runner.run.started", "run:r119", p, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := sub.Handle(ctx, work); err != nil {
		t.Fatalf("a non-decision was an error rather than a no-op: %v", err)
	}

	ruling := decision(t, m, "finding.judgement.set", "finding:f7", p,
		map[string]string{"from": "watching", "to": "dismissed", "reason": "third-party CDN"})
	if err := sub.Handle(ctx, ruling); err != nil {
		t.Fatal(err)
	}

	got, err := s.ForActor(ctx, "user:acct_sj", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("the ledger holds %d entries, want only the decision", len(got))
	}
	if got[0].Action != "finding.judgement.set" || got[0].Subject != "finding:f7" {
		t.Errorf("recorded %+v", got[0])
	}
}

func TestTheBeforeAndAfterSurviveVerbatimAndAreNotDecoded(t *testing.T) {
	s, _ := store(t)
	ctx := context.Background()
	m := ids()
	sub := auditapp.NewSubscriber(s, m, clock.System{})
	p := actor(t, m, "acct_sj")

	payload := map[string]string{"from": "watching", "to": "dismissed", "reason": "third-party CDN, not ours"}
	if err := sub.Handle(ctx, decision(t, m, "finding.judgement.set", "finding:f7", p, payload)); err != nil {
		t.Fatal(err)
	}

	got, err := s.ForSubject(ctx, "finding:f7", 10)
	if err != nil || len(got) != 1 {
		t.Fatalf("read back %d: %v", len(got), err)
	}
	var back map[string]string
	if err := json.Unmarshal(got[0].Detail, &back); err != nil {
		t.Fatalf("detail did not survive: %v", err)
	}
	if back["from"] != "watching" || back["to"] != "dismissed" || back["reason"] == "" {
		t.Errorf("an entry that cannot say what changed is useless in the dispute it exists for: %v", back)
	}
}

func TestScopeIsDerivedFromTheEnvelopeAndNeverDeclared(t *testing.T) {
	s, _ := store(t)
	ctx := context.Background()
	m := ids()
	sub := auditapp.NewSubscriber(s, m, clock.System{})

	tenanted, err := actor(t, m, "acct_sj").WithTenant("ws_ctf1")
	if err != nil {
		t.Fatal(err)
	}
	plain := actor(t, m, "acct_sj")

	cases := []struct {
		name   string
		event  events.Event
		want   domain.Scope
		wantWS string
	}{
		{"a tenanted act is workspace-scoped",
			decision(t, m, "target.scope.edited", "target:t1", tenanted, nil), domain.ScopeWorkspace, "ws_ctf1"},
		{"an account subject with no tenant is account-scoped",
			decision(t, m, "identity.account.archived", "account:a9", plain, nil), domain.ScopeAccount, ""},
		{"anything else is system-scoped",
			decision(t, m, "tool.definition.added", "tool:httpx", plain, nil), domain.ScopeSystem, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := domain.ScopeOf(tc.event); got != tc.want {
				t.Fatalf("ScopeOf gave %v, want %v", got, tc.want)
			}
			if err := sub.Handle(ctx, tc.event); err != nil {
				t.Fatal(err)
			}
			back, err := s.ForSubject(ctx, tc.event.Subject, 1)
			if err != nil || len(back) != 1 {
				t.Fatalf("read back %d: %v", len(back), err)
			}
			if back[0].Scope != tc.want || back[0].WorkspaceID != tc.wantWS {
				t.Errorf("stored scope=%v workspace=%q", back[0].Scope, back[0].WorkspaceID)
			}
		})
	}
}

// The at-least-once gate. decisions/0007 promises redelivery; the unique index
// is what stops it becoming a second claim that the thing happened.
func TestARedeliveredEventWritesOneRow(t *testing.T) {
	s, _ := store(t)
	ctx := context.Background()
	m := ids()
	sub := auditapp.NewSubscriber(s, m, clock.System{})
	e := decision(t, m, "report.generated", "report:halcyon-2026-09", actor(t, m, "acct_sj"), nil)

	for i := 0; i < 3; i++ {
		if err := sub.Handle(ctx, e); err != nil {
			t.Fatalf("delivery %d: %v", i+1, err)
		}
	}
	got, err := s.ForSubject(ctx, "report:halcyon-2026-09", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Errorf("three deliveries wrote %d rows", len(got))
	}
}

func TestDelegationIsRecordedAsBothActors(t *testing.T) {
	s, _ := store(t)
	ctx := context.Background()
	m := ids()
	sub := auditapp.NewSubscriber(s, m, clock.System{})

	client, err := provenance.User("acct_client")
	if err != nil {
		t.Fatal(err)
	}
	p, err := actor(t, m, "acct_sj").WithOnBehalfOf(client)
	if err != nil {
		t.Fatal(err)
	}
	if err := sub.Handle(ctx, decision(t, m, "report.generated", "report:r1", p, nil)); err != nil {
		t.Fatal(err)
	}
	got, _ := s.ForSubject(ctx, "report:r1", 1)
	if len(got) != 1 {
		t.Fatal("nothing recorded")
	}
	if got[0].Actor != "user:acct_sj" || got[0].OnBehalfOf != "user:acct_client" {
		t.Errorf("actor=%q on_behalf_of=%q — both halves are the record", got[0].Actor, got[0].OnBehalfOf)
	}
}

// The repository exposes no update and no delete. This asserts the half that
// holds when somebody reaches past it: there is no API for either, so the only
// way to check is to look at what the type offers.
func TestTheLedgerOffersNoWayToChangeAnEntry(t *testing.T) {
	s, _ := store(t)
	var i any = s
	if _, ok := i.(interface {
		Update(context.Context, domain.Entry) error
	}); ok {
		t.Error("the store exposes Update")
	}
	if _, ok := i.(interface {
		Delete(context.Context, id.ID) error
	}); ok {
		t.Error("the store exposes Delete")
	}
}
