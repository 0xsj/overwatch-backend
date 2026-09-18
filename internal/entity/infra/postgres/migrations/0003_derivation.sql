-- "READ OUT OF" — decisions/0003's second edge kind, finally buildable.
--
-- `0003` sealed two edge kinds and only one of them is a claim. An ATTRIBUTION
-- runs root-entity → fragment and carries claimant, confidence and state. A
-- DERIVATION runs fragment → fragment and carries NONE OF THEM:
--
--   > It carries no claimant, no confidence and no state, because nobody
--   > claimed anything: one source said so and the bytes are on disk. There is
--   > nothing on it to agree with.
--
-- The two shapes are DISJOINT rather than one shape with optional fields, which
-- is why this is its own table rather than a `kind` column on `attribution`.
--
-- IRREVERSIBLE: creates two tables.

create table entity.derivation (
    id           uuid        primary key,
    workspace_id uuid        not null,

    -- FROM is what it was read OUT OF; TO is what was read. `0003`'s example
    -- reads `{"from":"frg_2","to":"frg_9","label":"SAN entry"}` — the SAN was
    -- read out of the certificate's host.
    from_fragment_id uuid    not null,
    to_fragment_id   uuid    not null,

    -- The name of the ACT — `0003` requires one. It is the MAPPING'S FIELD
    -- NAME: `input`, `san_entry`, `commit_author`. A separate label column on
    -- the mapping would be a second place to write the same word, and the two
    -- would disagree the first time somebody edited one.
    label        text        not null,

    -- NOT NULL, both of them, and this is the record's sentence in the schema:
    --
    --   > A server may not emit a `derivation` it cannot source: an edge
    --   > without an `invocation_id` and an `artifact_id` is a similarity edge
    --   > wearing a costume, and it would arrive looking trustworthy.
    --
    -- CLAUDE.md forbids similarity edges outright. This constraint is what makes
    -- the ban structural rather than a matter of everyone remembering.
    invocation_id uuid       not null,
    artifact_id   uuid       not null,

    -- Which mapping VERSION read it, so an edge cites its parser the same way
    -- an observation does — and a corrected mapping's edges are separable from
    -- the ones it replaced.
    mapping_id   uuid        not null,

    created_at   timestamptz not null,

    constraint derivation_label_present check (label <> '' and length(label) <= 400),
    -- A fragment is not read out of itself. It is not an error a tool would
    -- make on purpose, and it is exactly what a mapping pointed at the wrong
    -- path produces — a self-edge on every record.
    constraint derivation_not_self check (from_fragment_id <> to_fragment_id)
);

-- ONE EDGE PER (from, to, label, invocation). A redelivered extraction must not
-- double the graph, and the outbox is at-least-once — 0007. The invocation is
-- in the key rather than out of it because the SAME pair found again by a LATER
-- run is a second sighting and worth its own row: that is how a reader sees the
-- edge is still true, which is `change` in CLAUDE.md's noun list.
create unique index derivation_once
    on entity.derivation (from_fragment_id, to_fragment_id, label, invocation_id);

-- The canvas draws outward from a fragment, so both directions are indexed.
create index derivation_from on entity.derivation (workspace_id, from_fragment_id);
create index derivation_to   on entity.derivation (workspace_id, to_fragment_id);

-- WHAT COULD NOT BE RESOLVED — decisions/0040 §5.
--
-- The tool named a value; no fragment exists for it. Two ordinary reasons, both
-- worth seeing:
--
--   a REFUSED candidate   the gate kept the step off that host, so no fragment
--                         was ever made from it — 0039
--   a FOLD mismatch       the tool echoed `ACME.test` and the fragment is
--                         `acme.test`
--
-- Same shape as `observation.unmapped`, for the same reason: `0035` records a
-- path nobody mapped rather than guessing at it, and this records a provenance
-- nobody could resolve rather than dropping it. "httpx cited 37 inputs, 35
-- resolved" is a sentence; a missing edge is not.
--
-- **The missing fragment is NOT created.** It would be manufactured from a tool
-- echoing its own input rather than from an observation about the target, and
-- it would then reach scope, coverage and the asset view as though something had
-- found it — resurrecting exactly the hosts a scope rule refused.
create table entity.derivation_unresolved (
    id            uuid        primary key,
    workspace_id  uuid        not null,
    invocation_id uuid        not null,
    mapping_id    uuid        not null,

    -- The half that DID resolve. An unresolved row still knows what was read.
    to_fragment_id uuid       not null,

    -- The half that did not, kept as the tool wrote it AND as it was looked up.
    -- Both, because a fold mismatch is one of the two reasons this row exists
    -- and storing only the folded form would hide it.
    from_kind     text        not null,
    from_value    text        not null,
    from_raw      text        not null,

    label         text        not null,
    created_at    timestamptz not null,

    constraint unresolved_from_present check (from_kind <> '' and from_value <> '')
);

create unique index unresolved_once
    on entity.derivation_unresolved (invocation_id, to_fragment_id, from_value, label);

create index unresolved_by_invocation
    on entity.derivation_unresolved (workspace_id, invocation_id);
