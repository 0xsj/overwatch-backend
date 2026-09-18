-- A finding is ONE PROBLEM ON ONE FRAGMENT — decisions/0041.
--
-- `nuclei` matching one template on one URL every night for a month is one
-- finding seen thirty times, not thirty findings. The board answers "what is
-- wrong with this estate"; one row per sighting turns it into a scan log where
-- twenty-nine rows out of thirty are noise, and where TRIAGE DOES NOT STICK —
-- a ruling made on Monday would face a fresh untriaged row on Tuesday, forever.

create schema if not exists finding;

create table finding.finding (
    id           uuid        primary key,
    workspace_id uuid        not null,

    -- THE IDENTITY, unique together. The TOOL is in it because two scanners'
    -- identifier spaces can collide and nothing here can know that one tool's
    -- `weak-cipher` means another's — merging them would assert that two
    -- signatures mean the same thing, which is the similarity claim CLAUDE.md
    -- bans outright.
    tool_id      uuid        not null,
    -- `signature` and not `template_id`: it is whatever the TOOL calls this
    -- class of problem, and `template-id` is nuclei's word for it.
    signature    text        not null,
    fragment_id  uuid        not null,

    -- A COPY of the fragment's tuple so a board renders without a join into a
    -- peer domain. Safe to denormalise because a fragment IS the tuple (0036)
    -- and is therefore immutable — this cannot go stale the way a mutable
    -- denormalised field would.
    fragment_kind  text      not null,
    fragment_value text      not null,

    -- open · triaged · resolved · dismissed — 0041 §3. They deliberately do NOT
    -- share the judgement quartet's words: 0009 is explicit that a finding is
    -- not a judgement, and reusing `unopened`/`watching` would invite exactly
    -- the collapse it refused.
    --
    -- RESOLVED AND DISMISSED MUST NEVER COLLAPSE. One is a change to the world,
    -- the other is a change of mind, and a client report cites them
    -- differently.
    state        text        not null,
    reason       text,
    decided_by   uuid,
    decided_at   timestamptz,

    -- A SCALAR beside its claim rather than nested inside it — 0004, because it
    -- is the sort key and the filter key for the board.
    severity     text        not null,

    -- 0004's `severity_by`. A rule carries NO confidence: a template asserting
    -- `high` is a category, not a probability, and storing 1.0 destroys the
    -- distinction permanently.
    claimant     text        not null,
    actor        uuid,
    confidence   double precision,
    basis        text,
    assessed_at  timestamptz not null,

    -- WHAT AN OVERRIDE REPLACED, kept whole. 0004: "an override never deletes
    -- what it overrode." A human raising a model's severity is the correction
    -- this product claims compounds, and a shape keeping only the outcome
    -- cannot see it.
    superseded_severity   text,
    superseded_claimant   text,
    superseded_actor      uuid,
    superseded_confidence double precision,
    superseded_basis      text,
    superseded_at         timestamptz,

    first_seen   timestamptz not null,
    last_seen    timestamptz not null,
    sightings    integer     not null default 1,

    -- The run that MOST RECENTLY saw it. The first sighting's citation is not
    -- kept: a finding is a live claim about the estate, and the evidence a
    -- reader wants is the newest rather than the oldest.
    --
    -- NOT NULL, both — 0003's rule one noun over. A finding this system cannot
    -- point back at an invocation and an artifact would arrive looking
    -- trustworthy.
    invocation_id uuid       not null,
    artifact_id   uuid       not null,
    mapping_id    uuid       not null,

    created_at   timestamptz not null,

    constraint finding_signature_present check (signature <> '' and length(signature) <= 400),
    constraint finding_fragment_present  check (fragment_kind <> '' and fragment_value <> ''),
    constraint finding_state_known    check (state in ('open', 'triaged', 'resolved', 'dismissed')),
    constraint finding_severity_known check (severity in ('info', 'low', 'medium', 'high', 'critical')),
    constraint finding_claimant_known check (claimant in ('rule', 'model', 'human')),

    -- 0004, in the schema: only a MODEL carries a confidence.
    constraint finding_confidence_on_model
        check ((claimant = 'model') = (confidence is not null)),
    constraint finding_confidence_range
        check (confidence is null or (confidence >= 0 and confidence <= 1)),
    -- A human names themselves; a rule and a model do not.
    constraint finding_actor_on_human
        check ((claimant = 'human') = (actor is not null)),

    -- DISMISSED REQUIRES A REASON; resolved does not. A fix needs no argument —
    -- the thing is gone. Deciding a real problem does not matter is a
    -- disagreement with the tool that found it, and one with no stated reason
    -- records THAT somebody disagreed without saying WHY THEY WERE RIGHT.
    constraint finding_dismissal_has_reason
        check (state <> 'dismissed' or (reason is not null and reason <> '')),
    -- Anything out of `open` was decided by somebody, at a time. Nothing closes
    -- a finding automatically — 0041 — so there is no system path to be an
    -- exception here.
    constraint finding_decided_paired
        check ((state = 'open') = (decided_by is null) and (decided_by is null) = (decided_at is null)),

    -- An override keeps the WHOLE prior claim or none of it.
    constraint finding_superseded_paired
        check ((superseded_severity is null) = (superseded_claimant is null)),
    constraint finding_superseded_has_basis
        check (superseded_severity is null or (basis is not null and basis <> '')),

    constraint finding_sightings_positive check (sightings >= 1),
    constraint finding_seen_ordered       check (last_seen >= first_seen)
);

-- THE IDENTITY. This index is the whole of 0041 §1 — it is what makes a rescan
-- a sighting rather than a new row, and therefore what makes triage stick.
create unique index finding_identity
    on finding.finding (workspace_id, tool_id, signature, fragment_id);

-- The board: newest and worst first, per engagement. Severity is text and sorts
-- alphabetically, which is wrong — the ORDER is applied in the query with a
-- case expression, because encoding it as an integer here would make every hand
-- query unreadable to serve one sort.
create index finding_board on finding.finding (workspace_id, state, last_seen desc);

-- 0009's badge: "this fragment has an open finding", counted per fragment.
-- Partial, because the badge only ever counts the live ones and an index over
-- resolved history would be paid for on every write to serve nothing.
create index finding_live_on_fragment
    on finding.finding (workspace_id, fragment_id)
    where state in ('open', 'triaged');

-- What a tool's mappings read BESIDE the identity — the name, the description,
-- the matcher, the curl command.
--
-- NOT AN OBSERVATION. An observation says what a source said about a SUBJECT,
-- and these are about the FINDING: filing `name: Log4j RCE` as an observation of
-- the URL would put it in that asset's field list, where it reads as a property
-- of the url. Same shape, different subject — and the subject is what an
-- observation IS.
create table finding.detail (
    id          uuid        primary key,
    finding_id  uuid        not null,

    field       text        not null,
    value       text        not null,

    mapping_id  uuid        not null,
    artifact_id uuid        not null,
    seen_at     timestamptz not null,

    constraint detail_field_present check (field <> '' and length(field) <= 200),
    constraint detail_value_present check (value <> '' and length(value) <= 8000)
);

-- ONE ROW PER (finding, field). A nightly rescan UPDATES rather than appends, so
-- this stays bounded while `sightings` grows — the alternative writes a row per
-- field per night forever to record that nothing changed.
create unique index detail_one_per_field on finding.detail (finding_id, field);
