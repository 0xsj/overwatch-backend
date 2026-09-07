-- entity's schema. Identity across sources, the fragments under it, and the
-- claim between them.
--
-- IRREVERSIBLE: creates a schema, three tables and a view.

create schema if not exists entity;

-- Identity across sources and time. NOT a fragment — every attribution runs from
-- here to one, which is why the canvas draws it apart from the nodes.
create table entity.entity (
    id           uuid        primary key,
    workspace_id uuid        not null,

    kind         text        not null,
    label        text        not null,

    -- Set when this entity is a target's ROOT — 0029's column finally has
    -- something to point at, and 0036 says a subscriber creates it.
    target_id    uuid,

    -- The judgement, as a value object — 0009. The IDENTICAL group appears on
    -- `fragment`, because Postgres cannot reference two tables from one column
    -- and every alternative to duplication was worse. `root` holds a test
    -- asserting the two groups have the same shape.
    judgement_state  text        not null default 'unopened',
    judgement_by     uuid,
    judgement_at     timestamptz,
    judgement_reason text,

    created_at   timestamptz not null,

    constraint entity_kind_present  check (kind <> ''),
    constraint entity_label_present check (label <> '' and length(label) <= 400),
    constraint entity_judgement_known check (
        judgement_state in ('unopened', 'triaged', 'watching', 'dismissed')
    ),
    -- Somebody ruling on it is what stops it being unopened.
    constraint entity_judgement_unopened check (
        judgement_state <> 'unopened' or (judgement_by is null and judgement_at is null)
    ),
    constraint entity_judgement_ruled check (
        judgement_state = 'unopened' or (judgement_by is not null and judgement_at is not null)
    ),
    -- "A dismissal with no reason reads as `never looked at` in six months."
    -- Triage deliberately does NOT require one — the asymmetry is 0009's rule.
    constraint entity_judgement_dismissal check (
        judgement_state <> 'dismissed' or (judgement_reason is not null and judgement_reason <> '')
    )
);

-- ONE root per target. The partial index is the rule: a target has at most one
-- root entity, and a redelivered `target.added` must not make a second.
create unique index entity_one_root_per_target on entity.entity (target_id)
    where target_id is not null;

create index entity_workspace on entity.entity (workspace_id, created_at desc);

-- A fragment IS the tuple (workspace, kind, value) — 0036.
create table entity.fragment (
    id           uuid        primary key,
    workspace_id uuid        not null,

    -- The shared kind vocabulary — 0034. `scope` holds the canonical copy and
    -- root/vocabulary_test.go is what stops these drifting.
    kind         text        not null,
    -- FOLDED. `ACME.test` and `acme.test` are one host, and a table admitting
    -- both would put the same asset on the list twice.
    value        text        not null,

    -- 0009's own recorded doubt, resolved by 0036: a `/24` typed into a scope
    -- rule is a fragment nobody observed.
    origin       text        not null,

    -- NULL for a manual fragment, and that is not a gap: nothing has seen it.
    first_seen   timestamptz,
    last_seen    timestamptz,
    -- Kept on the row rather than computed, because the asset list renders it
    -- per row and a count per row is a query per row.
    observations integer     not null default 0,

    judgement_state  text        not null default 'unopened',
    judgement_by     uuid,
    judgement_at     timestamptz,
    judgement_reason text,

    created_at   timestamptz not null,

    constraint fragment_kind_present  check (kind <> ''),
    constraint fragment_value_present check (value <> '' and value = lower(value)
                                             and length(value) <= 2000),
    constraint fragment_origin_known  check (origin in ('observed', 'manual')),
    constraint fragment_seen_paired   check (
        (first_seen is null) = (last_seen is null)
    ),
    constraint fragment_judgement_known check (
        judgement_state in ('unopened', 'triaged', 'watching', 'dismissed')
    ),
    constraint fragment_judgement_unopened check (
        judgement_state <> 'unopened' or (judgement_by is null and judgement_at is null)
    ),
    constraint fragment_judgement_ruled check (
        judgement_state = 'unopened' or (judgement_by is not null and judgement_at is not null)
    ),
    constraint fragment_judgement_dismissal check (
        judgement_state <> 'dismissed' or (judgement_reason is not null and judgement_reason <> '')
    )
);

-- THE DEDUP. This index is what makes a fragment a tuple rather than a row that
-- happens to have three columns, and it is what an observation joins on.
create unique index fragment_identity on entity.fragment (workspace_id, kind, value);

create index fragment_workspace on entity.fragment (workspace_id, last_seen desc nulls last);

-- ENTITY -> FRAGMENT, and IT IS A CLAIM — 0003. The only edge anybody accepts.
--
-- There is deliberately NO `derivation` table. 0003 seals its shape and nothing
-- can produce one: an edge means "this fragment was read out of that one" and
-- needs the upstream fragment known at extraction time, which needs the chain to
-- feed itself. 0003's own rule that "a server may not emit a derivation it
-- cannot source" means the first one written must already carry an invocation
-- and an artifact.
create table entity.attribution (
    id           uuid        primary key,
    workspace_id uuid        not null,

    entity_id    uuid        not null,
    fragment_id  uuid        not null,

    -- WHO PROPOSED IT, never rewritten — 0008.
    claimant     text        not null,
    -- The account when human; the RULE when a rule; null for a model, which has
    -- no id in this system yet.
    claimant_ref uuid,

    -- Present IFF the claimant is a model. A rule's assignment is a category and
    -- not a probability, and storing 1.0 destroys the distinction permanently.
    confidence   double precision,

    basis        text        not null,
    state        text        not null,

    -- 0008's invariant AS AMENDED BY 0036: decided_at moves with the state, and
    -- decided_by is set only when a PERSON decided. A rule deciding is still a
    -- decision; it has no account behind it, which is how `run.started_by`
    -- already spells "a schedule did this".
    decided_at   timestamptz,
    decided_by   uuid,
    decided_note text,

    created_at   timestamptz not null,

    constraint attribution_claimant_known check (claimant in ('rule', 'model', 'human')),
    constraint attribution_state_known    check (state in ('proposed', 'accepted', 'rejected')),
    constraint attribution_basis_present  check (basis <> ''),
    constraint attribution_confidence_iff_model check (
        (claimant = 'model') = (confidence is not null)
    ),
    constraint attribution_confidence_range check (
        confidence is null or (confidence >= 0 and confidence <= 1)
    ),
    constraint attribution_decided_at_iff_ruled check (
        (state = 'proposed') = (decided_at is null)
    ),
    -- A decider only where something was decided. The converse is NOT asserted:
    -- an accepted attribution with no decider is a RULE having decided, which
    -- is the whole of 0036 §3.
    constraint attribution_decider_only_when_ruled check (
        decided_by is null or state <> 'proposed'
    ),
    -- One claim per (entity, fragment). A redelivered subscriber event must not
    -- write a second, and two contradicting claims about one pair is a state
    -- nobody could render.
    unique (entity_id, fragment_id)
);

create index attribution_fragment on entity.attribution (fragment_id);
create index attribution_entity on entity.attribution (entity_id, state);
-- What the review queue reads: everything a person still has to rule on.
create index attribution_pending on entity.attribution (workspace_id, created_at)
    where state = 'proposed';

-- AN ASSET IS A VIEW — 0009 asked for exactly this and did not create it:
--
--   "A query for 'the assets' is `fragment WHERE kind IN (…) AND EXISTS
--    (accepted attribution to root entity)`, and nothing in the schema names
--    that predicate. A VIEW SHOULD CARRY THE NAME, so the word and the query
--    are the same object."
--
-- Nothing writes through it. The kind list is 0034's TARGETABLE facet, and the
-- join to a root entity is what "to the target's root entity" means — an
-- accepted attribution to some other entity does not make a fragment an asset.
create view entity.asset as
select
    f.id, f.workspace_id, f.kind, f.value, f.origin,
    f.first_seen, f.last_seen, f.observations,
    f.judgement_state, f.judgement_by, f.judgement_at, f.judgement_reason,
    f.created_at,
    a.id   as attribution_id,
    a.claimant,
    a.basis,
    e.id   as root_entity_id,
    e.target_id
from entity.fragment f
join entity.attribution a on a.fragment_id = f.id and a.state = 'accepted'
join entity.entity e on e.id = a.entity_id and e.target_id is not null
where f.kind in ('host', 'cidr', 'ip', 'asn', 'url', 'repo', 'email');
