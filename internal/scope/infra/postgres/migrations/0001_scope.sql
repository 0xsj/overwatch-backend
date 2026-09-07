-- scope's schema. One table, a gate discriminator, and append-only.
--
-- IRREVERSIBLE: creates a schema and a table. Reversing this is DROP SCHEMA.

create schema if not exists scope;

create table scope.rule (
    id           uuid        primary key,

    -- Every product row carries the engagement, non-null — 0005. Neither this
    -- nor target_id is a foreign key: no reference leaves this schema.
    workspace_id uuid        not null,
    target_id    uuid        not null,

    pattern      text        not null,
    polarity     text        not null,
    gate         text        not null,
    -- The kinds this rule can match, and the tool intensities it qualifies.
    -- Arrays rather than join tables: neither is ever queried across rules, and
    -- a rule's kinds are read only when the rule is.
    kinds        text[]      not null,
    tools        text[],

    created_by   uuid        not null,
    created_at   timestamptz not null,

    -- APPEND-ONLY — decisions/0030. There is no UPDATE that changes a pattern,
    -- a polarity, a gate or a kind: three surfaces cite a rule id and each
    -- captures it AT THE TIME, so a row whose pattern moved under a citation
    -- makes every one of those citations a lie.
    superseded_at timestamptz,
    superseded_by uuid,

    constraint rule_pattern_present check (pattern <> '' and pattern = lower(pattern)),
    constraint rule_pattern_length check (length(pattern) <= 400),
    constraint rule_polarity_known check (polarity in ('include', 'exclude')),
    constraint rule_gate_known     check (gate in ('spawn', 'claim')),
    constraint rule_kinds_present  check (cardinality(kinds) > 0),

    -- A CLAIM RULE CARRIES NO TOOLS — 0010. The field is absent rather than
    -- empty or wildcard, because there are no processes on that gate and an
    -- empty array reads as "no intensities" to a filter and "all" to anything
    -- that does not look.
    constraint rule_tools_iff_spawn check (
        (gate = 'spawn' and tools is not null and cardinality(tools) > 0) or
        (gate = 'claim' and tools is null)
    ),
    -- superseded_at and superseded_by are one fact in two columns.
    constraint rule_superseded_paired check (
        (superseded_at is null and superseded_by is null) or
        (superseded_at is not null and superseded_by is not null)
    )
);

-- The evaluator's read: one target's LIVE rules on one gate. It is the hot path
-- — every spawn is preceded by it — and the partial predicate is what keeps a
-- rule set that has been edited fifty times from reading fifty rows.
create index rule_live on scope.rule (target_id, gate)
    where superseded_at is null;

-- The editor's read: everything a target has ever had, newest first, because
-- superseded rules are the history the citations point at.
create index rule_target on scope.rule (target_id, created_at desc);
