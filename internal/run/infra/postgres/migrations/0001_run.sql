-- run's schema. A pipeline, its processes, and their bytes.
--
-- IRREVERSIBLE: creates a schema and three tables. Reversing this is DROP SCHEMA.

create schema if not exists run;

-- WORKSPACE_ID on all three tables — 0005, kept by 0031. A run is a claim about
-- a client, which is the side of 0031's test that keeps the narrow key.
create table run.run (
    id           uuid        primary key,
    workspace_id uuid        not null,
    target_id    uuid        not null,
    -- WHICH QUESTION was being asked. Coverage counts this — 0032. It is NOT a
    -- pointer to the chain: each invocation records the argv it ran, so a chain
    -- edited tomorrow does not rewrite what happened today.
    check_id     uuid        not null,

    state        text        not null,
    -- NULL when a schedule started it. "No person" is a fact, not a missing
    -- value.
    started_by   uuid,

    started_at   timestamptz not null,
    finished_at  timestamptz,
    version      integer     not null,

    constraint run_state_known    check (state in ('running', 'complete', 'stopped')),
    constraint run_finished_paired check ((state = 'running') = (finished_at is null))
);

create index run_workspace on run.run (workspace_id, started_at desc);
create index run_target on run.run (target_id, started_at desc);
-- The scheduler's read and the coverage read: the last run of one check over one
-- target. Descending, because both only ever want the newest.
create index run_check on run.run (check_id, target_id, started_at desc);

create table run.invocation (
    id           uuid        primary key,
    run_id       uuid        not null,
    workspace_id uuid        not null,

    -- Both recorded. The step says which node of the chain this was; the tool
    -- says what ran. Neither is a foreign key — both are other schemas.
    step_id      uuid        not null,
    tool_id      uuid        not null,
    -- Topological position, so a reader lays the run out without re-deriving
    -- the graph from a chain that may have moved since.
    sequence     integer     not null,

    phase        text        not null,

    -- WHAT RAN, resolved and verbatim — never the template (0032). Present even
    -- on a refusal, because "the command we would have run" is what a person
    -- reviews and a refusal with no argv is a scope proof nobody can read.
    argv         text[]      not null,
    -- The RESOLVED path. Which `httpx` ran matters when there are two on PATH.
    -- `binary` is a reserved SQL word, like the schema `check` was two hours
    -- ago. Same class of failure, second instance in one session — see the note
    -- on reserved identifiers.
    binary_path  text,

    -- NULL means NO PROCESS EVER EXISTED, not "unknown". CLAUDE.md's
    -- `skipped vs failed` pair, held by a constraint rather than a convention.
    exit_code    integer,
    signal       text,

    -- The scope proof — 0010 and 0030. One of the three surfaces that cite a
    -- rule id, and the reason a rule is superseded rather than edited.
    -- NULL WITH A REASON is "nothing permitted it", which is a different fact
    -- from "a rule excluded it".
    refusal_rule   uuid,
    refusal_reason text,

    skipped_because text,
    -- execx's reason when the tool could not start at all. A tool off PATH
    -- otherwise looks like silence — CLAUDE.md's `health` noun.
    unavailable    text,

    started_at   timestamptz,
    finished_at  timestamptz,
    duration_ms  bigint      not null default 0,

    constraint invocation_phase_known check (
        phase in ('pending', 'running', 'ok', 'failed', 'refused', 'skipped')
    ),
    constraint invocation_argv_present check (cardinality(argv) > 0),
    -- THE CONSTRAINT THIS TABLE EXISTS FOR. An exit code may only be present
    -- where a process ran. `failed` is on both sides: a tool off PATH failed and
    -- never exited.
    constraint invocation_exit_needs_a_process check (
        exit_code is null or phase in ('ok', 'failed')
    ),
    constraint invocation_refusal_shape check (
        (phase = 'refused') = (refusal_reason is not null)
    ),
    -- A rule may only be cited by a refusal. The converse is NOT asserted:
    -- `refused` with no rule is 0010's default — nothing permitted it.
    constraint invocation_rule_only_on_refusal check (
        refusal_rule is null or phase = 'refused'
    ),
    constraint invocation_skip_shape check (
        (phase = 'skipped') = (skipped_because is not null)
    )
);

create index invocation_run on run.invocation (run_id, sequence);
create index invocation_workspace on run.invocation (workspace_id, finished_at desc);
-- What "which runs did this rule refuse" reads, and it is the query that makes
-- an append-only scope ledger worth having.
create index invocation_refusal on run.invocation (refusal_rule)
    where refusal_rule is not null;

-- The ROW. pkg/blob holds the bytes; this holds what a citation points at.
create table run.artifact (
    id            uuid        primary key,
    workspace_id  uuid        not null,
    invocation_id uuid        not null,

    -- Two artifacts per process, not one. execx caps and truncates each stream
    -- separately — "a tool that fills stderr with warnings has not lost its
    -- findings" — and one concatenated blob makes that unrecoverable.
    stream        text        not null,

    -- `sha256:<hex>`. The algorithm travels with the digest so the day sha256 is
    -- not enough there is a field to widen rather than a convention to find.
    hash          text        not null,
    -- ZERO IS A RESULT: it ran and wrote an empty artifact. "Nothing was
    -- written" is the ABSENCE of this row.
    bytes         bigint      not null,
    -- A fact about the artifact, not something to infer from a length: after
    -- the fact, a length and a limit look identical.
    truncated     boolean     not null default false,
    -- What the bytes are SAID to be, from the argv. Never sniffed — guessing it
    -- from content is how a record acquires a claim nobody made.
    media_type    text,

    created_at    timestamptz not null,

    constraint artifact_stream_known check (stream in ('stdout', 'stderr')),
    constraint artifact_hash_present check (hash <> ''),
    constraint artifact_bytes_positive check (bytes >= 0),
    -- One artifact per stream per invocation. A retried write of the same bytes
    -- is one blob (content-addressed) and must be one row.
    unique (invocation_id, stream)
);

create index artifact_invocation on run.artifact (invocation_id);
-- Two invocations that produced identical bytes cite ONE blob and keep two rows.
-- This is what makes "who else saw these bytes" answerable.
create index artifact_hash on run.artifact (hash);
