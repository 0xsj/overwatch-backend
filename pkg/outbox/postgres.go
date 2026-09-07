package outbox

import (
	"context"
	"encoding/json"
	"time"

	"github.com/0xsj/overwatch-backend/pkg/errors"
	"github.com/0xsj/overwatch-backend/pkg/events"
	"github.com/0xsj/overwatch-backend/pkg/id"
	"github.com/0xsj/overwatch-backend/pkg/postgres"
	"github.com/0xsj/overwatch-backend/pkg/provenance"
)

// Migrations is this package's own schema. It is passed to postgres.Migrate by
// the composition root rather than registered globally, for the reason
// decisions/0002 gives: a test migrates a chosen set into a chosen schema.
var Migrations = []postgres.Migration{{
	Name: "0001_outbox.sql",
	SQL: `
create table outbox (
	id           uuid        primary key,
	name         text        not null,
	occurred_at  timestamptz not null,
	provenance   jsonb       not null,
	payload      jsonb,
	attempts     int         not null default 0,
	due_at       timestamptz not null default now(),
	buried_at    timestamptz,
	last_error   text,
	written_at   timestamptz not null default now()
);

-- The claim predicate, and the only index that matters: a partial index over
-- exactly the rows a dispatcher looks for. A full index on due_at would carry
-- every buried row forever, in a table whose whole point is being small.
create index outbox_due on outbox (due_at, written_at) where buried_at is null;
create index outbox_buried on outbox (buried_at) where buried_at is not null;
`,
}, {
	Name: "0002_outbox_subject.sql",
	SQL: `
-- decisions/0013 put the subject in the envelope, so the outbox has to carry
-- it. A second file rather than an edit to 0001: the migrator refuses an
-- applied migration whose checksum moved, and that guard is worth more than
-- the tidiness of one file for a table nothing has shipped yet.
--
-- Backfilled to '' rather than left null, then made NOT NULL. There are no rows
-- outside a test database, so the backfill is a formality — and writing it as
-- though there were is how the pattern is right the first time it is not.
alter table outbox add column if not exists subject text not null default '';
alter table outbox alter column subject drop default;
`,
}, {
	Name: "0003_outbox_decision.sql",
	SQL: `
-- decisions/0014: an event says whether a person is accountable for it, because
-- a subscriber cannot derive that. Two subscribers read the same row and apply
-- different predicates to this column.
alter table outbox add column if not exists decision boolean not null default false;
alter table outbox alter column decision drop default;
`,
}}

// Postgres is the adapter that runs. It takes the pool rather than a DBTX so
// that Add can ask for the CONTEXT's transaction — which is the whole property
// decisions/0007 turns on, and the one a stored DBTX would quietly discard.
// NotifyChannel is what Add signals on and what a waker listens to. One name,
// declared where the insert is, so the two halves cannot drift.
const NotifyChannel = "overwatch_outbox"

type Postgres struct {
	db *postgres.Pool
}

func NewPostgres(db *postgres.Pool) *Postgres {
	if db == nil {
		panic("outbox: NewPostgres with a nil Pool")
	}
	return &Postgres{db: db}
}

func (p *Postgres) Add(ctx context.Context, evs ...events.Event) error {
	for _, e := range evs {
		prov, err := json.Marshal(e.Provenance)
		if err != nil {
			return errors.Wrap(err, errors.Internal, "outbox: encode provenance")
		}
		// on conflict do nothing: a retried command re-publishing the same event
		// id writes one row, which is what makes the event id the dedup key all
		// the way down rather than only at the handler.
		_, err = p.db.DB(ctx).Exec(ctx, `
			insert into outbox (id, name, subject, decision, occurred_at, provenance, payload)
			values ($1, $2, $3, $4, $5, $6, $7)
			on conflict (id) do nothing`,
			e.ID, e.Name, e.Subject, e.Decision, e.OccurredAt, prov, []byte(e.Payload))
		if err != nil {
			return postgres.Translate(ctx, err, "outbox: add "+e.Name)
		}
	}

	// NOTIFY inside the caller's transaction, so it is delivered ON COMMIT and
	// never for an event that rolled back. Postgres collapses identical
	// notifications within one transaction, so a batch of ten wakes once.
	//
	// A failure here is deliberately NOT returned: the rows are written and the
	// ticker will find them. Failing the caller's transaction because a
	// latency optimisation did not fire would trade a correct outcome for a
	// faster one.
	_, _ = p.db.DB(ctx).Exec(ctx, "select pg_notify($1, '')", NotifyChannel)
	return nil
}

func (p *Postgres) Claim(ctx context.Context, n int, now time.Time) ([]Pending, error) {
	// SKIP LOCKED so a second dispatcher takes different rows rather than
	// blocking on these. The update is the claim: due_at moves out of reach for
	// the length of one delivery, so a crashed dispatcher's rows come back on
	// their own rather than needing a reaper.
	rows, err := p.db.DB(ctx).Query(ctx, `
		update outbox set due_at = $2
		where id in (
			select id from outbox
			where buried_at is null and due_at <= $1
			order by written_at
			for update skip locked
			limit $3
		)
		returning id, name, subject, decision, occurred_at, provenance, payload, attempts`,
		now, now.Add(claimLease), n)
	if err != nil {
		return nil, postgres.Translate(ctx, err, "outbox: claim")
	}
	defer rows.Close()

	var out []Pending
	for rows.Next() {
		var (
			e    events.Event
			prov []byte
			pay  []byte
			att  int
		)
		if err := rows.Scan(&e.ID, &e.Name, &e.Subject, &e.Decision, &e.OccurredAt, &prov, &pay, &att); err != nil {
			return nil, postgres.Translate(ctx, err, "outbox: scan a claim")
		}
		var pv provenance.Provenance
		if err := json.Unmarshal(prov, &pv); err != nil {
			return nil, errors.Wrap(err, errors.Internal, "outbox: decode provenance for "+e.Name)
		}
		e.Provenance = pv
		e.Payload = pay
		out = append(out, Pending{Event: e, Attempts: att})
	}
	return out, postgres.Translate(ctx, rows.Err(), "outbox: claim")
}

// claimLease is how long a claimed row is invisible to other dispatchers. Long
// enough that a slow handler is not re-delivered under itself; short enough that
// a killed process does not strand its batch for an operator to notice.
const claimLease = 5 * time.Minute

func (p *Postgres) Delivered(ctx context.Context, ids []id.ID) error {
	if len(ids) == 0 {
		return nil
	}
	_, err := p.db.DB(ctx).Exec(ctx, `delete from outbox where id = any($1)`, ids)
	return postgres.Translate(ctx, err, "outbox: drain")
}

func (p *Postgres) Failed(ctx context.Context, ids []id.ID, reason string, retryAt time.Time) error {
	if len(ids) == 0 {
		return nil
	}
	if retryAt.IsZero() {
		_, err := p.db.DB(ctx).Exec(ctx, `
			update outbox set attempts = attempts + 1, last_error = $2, buried_at = now()
			where id = any($1)`, ids, reason)
		return postgres.Translate(ctx, err, "outbox: bury")
	}
	_, err := p.db.DB(ctx).Exec(ctx, `
		update outbox set attempts = attempts + 1, last_error = $2, due_at = $3
		where id = any($1)`, ids, reason, retryAt)
	return postgres.Translate(ctx, err, "outbox: defer")
}

func (p *Postgres) Depth(ctx context.Context, now time.Time) (Level, error) {
	var (
		l      Level
		oldest *time.Time
	)
	err := p.db.DB(ctx).QueryRow(ctx, `
		select
			count(*) filter (where buried_at is null),
			count(*) filter (where buried_at is not null),
			min(occurred_at) filter (where buried_at is null)
		from outbox`).Scan(&l.Pending, &l.Buried, &oldest)
	if err != nil {
		return Level{}, postgres.Translate(ctx, err, "outbox: depth")
	}
	if oldest != nil {
		l.Oldest = now.Sub(*oldest)
	}
	return l, nil
}
