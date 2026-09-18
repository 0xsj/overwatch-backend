-- A report is a stored CONFIGURATION until it is issued — decisions/0042.
--
-- Then it is BYTES: `pkg/blob` holds them content-addressed, exactly the way an
-- artifact is held, and for the same reason. The hash is what makes "the client
-- received exactly this" a checkable claim rather than an assurance.

create schema if not exists report;

create table report.report (
    id           uuid        primary key,
    workspace_id uuid        not null,
    -- PER TARGET, because "the engagement written up" is one client's estate
    -- and a workspace can hold more than one.
    target_id    uuid        not null,

    title        text        not null,
    -- FREE TEXT and not an account id, deliberately. It is the name that
    -- appears on the document — a firm's, a partner's — and it is not who
    -- pressed the button. Who pressed it is `issued_by` on the revision, which
    -- is a record and never a byline.
    prepared_by  text,

    -- Dates a person TYPES. Nothing derives them from the first and last run: a
    -- report covering "Q3" is a claim about a contract rather than about data.
    period_start date,
    period_end   date,

    -- Counts what has been issued. Zero means this never left the building,
    -- which is a different fact from "issued and then edited".
    revisions    integer     not null default 0,

    created_by   uuid        not null,
    created_at   timestamptz not null,
    updated_at   timestamptz not null,

    constraint report_title_present check (title <> '' and length(title) <= 200),
    constraint report_period_ordered
        check (period_start is null or period_end is null or period_end >= period_start),
    constraint report_revisions_positive check (revisions >= 0)
);

create index report_for_workspace on report.report (workspace_id, target_id, created_at desc);

-- THE ENABLED SET, STORED — 0042 Section 2.
--
-- Defaults are defaults; a report configured a year ago with the invocation log
-- ON must still say so after the default moves. "What a given client received is
-- a fact about that document and has to be recoverable from it."
--
-- A row per (report, section) rather than a boolean column per section, because
-- the section list is an enum that will grow and a schema change per section is
-- how a toggle ends up in code instead.
create table report.section (
    report_id uuid    not null,
    section   text    not null,
    enabled   boolean not null,

    primary key (report_id, section),

    constraint section_known check (section in (
        'scope_and_method', 'attribution_evidence', 'asset_inventory',
        'findings_by_severity', 'coverage', 'invocation_log', 'raw_artifacts',
        'engagement_notes'
    )),
    -- COVERAGE CANNOT BE TURNED OFF. "This section exists because a report that
    -- omits it implies a completeness nobody achieved" — the product's thesis,
    -- and making it a toggle would make refusing it optional. The domain holds
    -- the same rule so the error names it rather than naming a constraint.
    constraint section_coverage_mandatory
        check (section <> 'coverage' or enabled)
);

-- ONE ISSUED DOCUMENT. Re-issuing writes a SECOND revision beside the first,
-- never over it: a report sent in January and re-sent in March is two documents,
-- and a dispute about a number in the January one is answerable.
create table report.revision (
    id           uuid        primary key,
    report_id    uuid        not null,
    -- Carried so a revision can be read WITHOUT first reading its report, which
    -- is the whole of a client's access path.
    workspace_id uuid        not null,

    number       integer     not null,

    -- The content address. NOT NULL: a revision this system cannot address is
    -- not a deliverable, the same rule 0003 applies to an edge and 0041 to a
    -- finding.
    hash         text        not null,
    bytes        bigint      not null,
    media_type   text        not null,

    -- WHAT THIS REVISION CONTAINED, in order — a copy of the enabled set at the
    -- moment it was issued, because the report's own set may have moved since.
    sections     text[]      not null,

    issued_by    uuid        not null,
    issued_at    timestamptz not null,

    constraint revision_number_positive check (number >= 1),
    constraint revision_hash_present    check (hash <> ''),
    constraint revision_bytes_positive  check (bytes >= 0),
    constraint revision_has_sections    check (cardinality(sections) > 0)
);

-- A number is assigned ONCE within a report. This is what makes "revision 2"
-- mean one document forever.
create unique index revision_number on report.revision (report_id, number);

create index revision_for_report on report.revision (report_id, number desc);
