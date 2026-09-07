-- observation's schema. What one source said, per field, with lineage.
--
-- IRREVERSIBLE: creates a schema and two tables. Reversing this is DROP SCHEMA.

create schema if not exists observation;

-- WORKSPACE_ID, non-null — 0005 kept by 0031. An observation is a claim about a
-- client, which is the side of 0031's test that keeps the narrow key.
create table observation.observation (
    id            uuid        primary key,
    workspace_id  uuid        not null,

    -- What it is ABOUT, carried INLINE rather than as a fragment reference —
    -- 0035. `fragment` is UNBUILT and will be a dedup over exactly these two
    -- columns, so it adds a column here rather than changing what a row means.
    subject_kind  text        not null,
    subject_value text        not null,

    -- What was said. `field` is named by the MAPPING and never inferred from
    -- the source's own key: a guessed name is a value nobody was asked for.
    field         text        not null,
    value         text        not null,

    -- THE LINEAGE. Every value walks backwards to the parser version, the raw
    -- bytes and the exact command; the scope rule that permitted the command
    -- hangs off the invocation. None is a foreign key — all three are other
    -- schemas.
    invocation_id uuid        not null,
    artifact_id   uuid        not null,
    mapping_id    uuid        not null,
    -- Captured here as well as reachable through mapping_id, because it is what
    -- a correction changes and a reader comparing two observations of one field
    -- wants the number without a second read.
    mapping_version integer   not null,

    -- WHEN THE TOOL RAN, not when extraction happened. Re-reading an old
    -- artifact under a corrected mapping must not make it look fresh.
    observed_at   timestamptz not null,
    recorded_at   timestamptz not null,

    constraint observation_subject_present check (subject_kind <> '' and subject_value <> ''),
    constraint observation_field_present   check (field <> '' and length(field) <= 200),
    constraint observation_value_present   check (value <> '' and length(value) <= 4000),
    constraint observation_version_positive check (mapping_version >= 1),
    -- observed_at may be BEFORE recorded_at by any amount (a re-extraction) and
    -- must never be after it: a source cannot have said something after we
    -- wrote down that it did.
    constraint observation_observed_before_recorded check (observed_at <= recorded_at)
);

-- The asset drawer's read: everything known about one subject, newest first.
create index observation_subject on observation.observation
    (workspace_id, subject_kind, subject_value, field, observed_at desc);

-- The run detail's read, and what makes `invocation.observations` a number.
create index observation_invocation on observation.observation (invocation_id);

-- What a CORRECTION will need: which observations did this mapping version
-- produce. Nothing re-extracts yet and the index is cheap; without it, the first
-- correction is a sequential scan of the largest table in the product.
create index observation_mapping on observation.observation (mapping_id, mapping_version);

-- A leaf path the source emitted that no live mapping claimed.
--
-- **A RECORD, NOT A FAILURE.** A tool that grew two fields after an upgrade has
-- not started lying; it has started saying something nobody has taught this
-- system to read. Inferring a field name from a JSON key is exactly the guess
-- that turns an observation into a fact.
create table observation.unmapped (
    id            uuid        primary key,
    workspace_id  uuid        not null,
    invocation_id uuid        not null,
    artifact_id   uuid        not null,

    -- The leaf's spelling, in the same form a mapping expression takes — so the
    -- fix is copy-and-paste rather than translation.
    path          text        not null,
    -- How many records carried it. One occurrence in ten thousand is a
    -- different question from ten thousand.
    seen          integer     not null,
    -- ONE value, so a person can tell what the field is without opening the
    -- artifact. Deliberately a sample and not a distinct-value set: a set is a
    -- summary of a field nobody has decided to keep.
    sample        text,

    recorded_at   timestamptz not null,

    constraint unmapped_path_present check (path <> '' and length(path) <= 400),
    constraint unmapped_seen_positive check (seen >= 1),
    -- One row per path per artifact. Re-extracting the same bytes must not
    -- double the LEFT ALONE count.
    unique (artifact_id, path)
);

create index unmapped_invocation on observation.unmapped (invocation_id);
-- What the Extraction Quality panel groups by: which paths does this engagement
-- keep leaving alone.
create index unmapped_workspace on observation.unmapped (workspace_id, path);
