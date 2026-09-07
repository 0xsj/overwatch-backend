-- check's schema. A question, and the chain that answers it.
--
-- THE SCHEMA IS `checks` AND THE PACKAGE IS `check`. `check` is the SQL
-- constraint keyword, so the unquoted name is a syntax error — see the note on
-- postgres.Schema.
--
-- IRREVERSIBLE: creates a schema and three tables. Reversing this is DROP SCHEMA.

create schema if not exists checks;

-- ORG_ID AND NOT WORKSPACE_ID — decisions/0031 and 0032. "What ports are open"
-- is a question the firm knows how to ask, and it is the same question for every
-- client. A per-engagement "do not run this here" is deliberately absent: that
-- is `scope`, which refuses the spawn by a rule somebody wrote and can be shown.
create table checks.check (
    id          uuid        primary key,
    org_id      uuid        not null,

    name        text        not null,
    -- The QUESTION, stored apart from the chain, because coverage counts
    -- questions rather than tools — 0032. Rewiring a chain leaves the coverage
    -- cell alone; changing the question does not.
    question    text        not null,

    -- The applicability matrix decisions/0011 created and did not fill in.
    -- Non-empty: a check that applies to nothing has no cell anywhere.
    --
    -- It is DATA A PERSON TYPES and not a rule anything can check. 0011 predicts
    -- the first mistake lands on `ip` versus `host`.
    applies_to  text[]      not null,

    -- NULL means "when somebody asks" — a kind of check, not an unset field.
    -- READ BY YOU has no clock and never goes stale.
    interval_seconds integer,

    enabled     boolean     not null,

    status      text        not null,
    version     integer     not null,
    created_by  uuid        not null,
    created_at  timestamptz not null,
    updated_at  timestamptz not null,
    archived_at timestamptz,

    constraint check_name_present     check (name <> '' and length(name) <= 120),
    constraint check_question_present check (question <> '' and length(question) <= 400),
    constraint check_applies_present  check (cardinality(applies_to) > 0),
    constraint check_applies_known    check (
        applies_to <@ array['domain', 'host', 'ip', 'cidr', 'asn', 'url']::text[]
    ),
    -- Zero is not "on demand" — NULL is. A zero-second interval would be a
    -- schedule that fires continuously, which nothing wants and nothing refuses.
    constraint check_interval_positive check (interval_seconds is null or interval_seconds > 0),
    constraint check_status_known      check (status in ('active', 'archived')),
    constraint check_archived_paired   check ((status = 'archived') = (archived_at is not null))
);

create unique index check_live_name on checks.check (org_id, lower(name))
    where status <> 'archived';

create index check_org on checks.check (org_id, created_at);

-- A step is a NODE and its id is STABLE across an edit — 0032. An invocation
-- will record which step produced it, so a save that deletes every row and
-- re-inserts would churn ids a run already recorded. The store diffs.
create table checks.step (
    id       uuid not null primary key,
    check_id uuid not null,
    -- Not a foreign key: `tool` is another schema and no reference leaves this
    -- one. The command checks the tool is the same org's.
    tool_id  uuid not null,

    -- Where somebody dragged this node. `pinned` is separate because (0, 0) and
    -- "never moved" are different facts and one column cannot hold both.
    x        integer not null default 0,
    y        integer not null default 0,
    pinned   boolean not null default false
);

create index step_check on checks.step (check_id);

-- The only edge kind here, and it is not a claim — decisions/0003 keeps two
-- edge kinds apart on the ENTITY canvas; this canvas has one, and an edge means
-- "these bytes were fed to that program".
create table checks.flow (
    check_id  uuid not null,
    from_step uuid not null,
    to_step   uuid not null,

    -- A duplicate edge is a second copy of one fact. The primary key is what
    -- refuses it; the domain refuses it too, so the editor gets a message rather
    -- than a constraint violation.
    primary key (check_id, from_step, to_step),
    constraint flow_not_self check (from_step <> to_step)
);

create index flow_check on checks.flow (check_id);

-- ACYCLICITY IS NOT HERE. It is a property of a graph and not of a row, and a
-- trigger that walks the edge set on every insert would make saving a
-- twelve-step chain quadratic to protect against something the domain already
-- refuses. The domain's Chain.Validate is the enforcement, and the store writes
-- a chain in one transaction so a partial one is never visible.
