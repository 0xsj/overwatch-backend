-- What a step was pointed at, one row per thing — decisions/0039.
--
-- A STEP IS ONE INVOCATION however many things it touches, because `httpx -l
-- hosts.txt` is one process and modelling forty hosts as forty invocations would
-- be a record of something that did not happen. So `0033` §1 survives: every
-- step still gets exactly one invocation row at plan time.
--
-- THE CANDIDATE IS WHERE THE SCOPE PROOF LIVES. An array of subjects on the
-- invocation was the obvious widening and it is refused: the REFUSED candidates
-- would then have nowhere to live, and "we would have looked at these three and
-- a rule said no" is `0010`'s whole purpose and the reason `0030` keeps rules
-- append-only.
--
-- IRREVERSIBLE: creates a table, and drops the two columns it replaces.

create table run.candidate (
    id            uuid        primary key,
    workspace_id  uuid        not null,
    run_id        uuid        not null,
    invocation_id uuid        not null,

    -- The shared kind vocabulary — 0034. For a SOURCE step this is the target
    -- and there is exactly one row; for a downstream step it is the subject of
    -- an upstream observation.
    kind          text        not null,
    -- FOLDED, matching `entity.fragment.value` and `observation.subject_value`.
    -- 0037 named the fold mismatch as its own quiet failure and it applies here
    -- with more force: this is what coverage joins on.
    value         text        not null,

    -- The gate is asked PER CANDIDATE — 0010 requires it, each host is
    -- permitted or not on its own — and the permitted subset is what the argv
    -- carries.
    --
    --   true    it was in the argv. COVERAGE reads these
    --   false   a rule refused it.   THE SCOPE PROOF reads these
    permitted     boolean     not null,
    -- Null when nothing permitted it rather than a rule having excluded it —
    -- 0010's default, and a different fact from an exclusion.
    refusal_rule  uuid,
    refusal_reason text,

    created_at    timestamptz not null,

    constraint candidate_kind_present  check (kind <> ''),
    constraint candidate_value_present check (value <> '' and value = lower(value)
                                              and length(value) <= 2000),
    -- A rule may only be cited by a refusal, and a refusal always says why.
    -- The converse of the first is NOT asserted: refused with no rule is
    -- "nothing permitted it".
    constraint candidate_rule_only_on_refusal check (permitted = false or refusal_rule is null),
    constraint candidate_refusal_has_reason   check (permitted = true or refusal_reason is not null),
    -- One row per thing per invocation. A retried resolution must not double
    -- the coverage denominator.
    unique (invocation_id, kind, value)
);

-- What COVERAGE reads: the permitted candidates of an engagement, by subject.
create index candidate_covered on run.candidate (workspace_id, kind, value)
    where permitted;

-- What the SCOPE PROOF reads: which spawns a rule refused, per candidate. It is
-- the other half of `invocation_refusal`, which records a whole step refused.
create index candidate_refused on run.candidate (refusal_rule)
    where refusal_rule is not null;

create index candidate_invocation on run.candidate (invocation_id);

-- THE COLUMNS THIS REPLACES, added four hours ago by 0037.
--
-- They were right when a step touched one thing, and this record makes that the
-- special case. The reason is written here rather than only in 0039 because a
-- reader finding a dropped column wants it at the drop.
alter table run.invocation drop constraint invocation_subject_paired;
alter table run.invocation drop constraint invocation_subject_folded;
drop index if exists run.invocation_subject;
alter table run.invocation drop column subject_kind;
alter table run.invocation drop column subject_value;
