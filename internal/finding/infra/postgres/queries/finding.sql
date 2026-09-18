-- name: UpsertFinding :one
-- THE WHOLE OF 0041 §1, as one statement. A rescan is a SIGHTING, not a row.
--
-- The conflict target is the identity — (workspace, tool, signature, fragment) —
-- and what it does on conflict is what makes triage stick:
--
--   sightings   ADDS. Two sightings are two sightings
--   last_seen   greatest(). A re-extraction of an old artifact must not make a
--               stale run look like the current one
--   first_seen  least(). And a re-extraction of an OLDER one legitimately
--               moves it back
--   state       UNTOUCHED, except that a `resolved` finding seen again reopens.
--               A human's ruling is about the problem, not about the run that
--               noticed it
--   severity    UNTOUCHED. The tool says `high` again tonight; overwriting a
--               human's override with the template's opinion every night is the
--               same failure one field over
--
-- The citation follows the newest evidence, which is why the three source
-- columns move only when `excluded.last_seen` wins.
insert into finding.finding (
    id, workspace_id, tool_id, signature, fragment_id,
    fragment_kind, fragment_value, state, severity,
    claimant, actor, confidence, basis, assessed_at,
    first_seen, last_seen, sightings,
    invocation_id, artifact_id, mapping_id, created_at
) values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21)
on conflict (workspace_id, tool_id, signature, fragment_id) do update
set sightings  = finding.finding.sightings + 1,
    last_seen  = greatest(finding.finding.last_seen, excluded.last_seen),
    first_seen = least(finding.finding.first_seen, excluded.first_seen),
    state = case
        -- A RESOLVED finding seen again is OPEN. There is no `regressed` state
        -- — 0041 accepted that cost — so a fix that did not hold reads as a new
        -- finding, and the evidence a reader has is an old first_seen beside a
        -- large sightings count.
        when finding.finding.state = 'resolved' then 'open'
        else finding.finding.state end,
    reason = case when finding.finding.state = 'resolved' then null else finding.finding.reason end,
    decided_by = case when finding.finding.state = 'resolved' then null else finding.finding.decided_by end,
    decided_at = case when finding.finding.state = 'resolved' then null else finding.finding.decided_at end,
    invocation_id = case when excluded.last_seen > finding.finding.last_seen
                         then excluded.invocation_id else finding.finding.invocation_id end,
    artifact_id   = case when excluded.last_seen > finding.finding.last_seen
                         then excluded.artifact_id else finding.finding.artifact_id end,
    mapping_id    = case when excluded.last_seen > finding.finding.last_seen
                         then excluded.mapping_id else finding.finding.mapping_id end
returning id, workspace_id, tool_id, signature, fragment_id, fragment_kind,
          fragment_value, state, reason, decided_by, decided_at, severity,
          claimant, actor, confidence, basis, assessed_at,
          superseded_severity, superseded_claimant, superseded_actor,
          superseded_confidence, superseded_basis, superseded_at,
          first_seen, last_seen, sightings, invocation_id, artifact_id,
          mapping_id, created_at, (xmax = 0) as opened;

-- name: FindingByID :one
-- BOTH IDS. A finding read without its engagement is another client's.
select id, workspace_id, tool_id, signature, fragment_id, fragment_kind,
       fragment_value, state, reason, decided_by, decided_at, severity,
       claimant, actor, confidence, basis, assessed_at,
       superseded_severity, superseded_claimant, superseded_actor,
       superseded_confidence, superseded_basis, superseded_at,
       first_seen, last_seen, sightings, invocation_id, artifact_id,
       mapping_id, created_at
from finding.finding where id = $1 and workspace_id = $2;

-- name: FindingsForWorkspace :many
-- The board. WORST FIRST, then newest — a findings screen sorted by time puts a
-- critical from Tuesday under an info from this morning.
--
-- The severity order is a CASE here rather than an integer column, because an
-- integer would make every hand-written query about this table unreadable to
-- serve one sort. It is the only place the order exists in SQL and the domain
-- holds the same one.
select id, workspace_id, tool_id, signature, fragment_id, fragment_kind,
       fragment_value, state, reason, decided_by, decided_at, severity,
       claimant, actor, confidence, basis, assessed_at,
       superseded_severity, superseded_claimant, superseded_actor,
       superseded_confidence, superseded_basis, superseded_at,
       first_seen, last_seen, sightings, invocation_id, artifact_id,
       mapping_id, created_at
from finding.finding
where workspace_id = $1
  and (sqlc.narg(state)::text is null or state = sqlc.narg(state)::text)
  and (sqlc.narg(fragment)::uuid is null or fragment_id = sqlc.narg(fragment)::uuid)
order by case severity
    when 'critical' then 0 when 'high' then 1 when 'medium' then 2
    when 'low' then 3 else 4 end,
    last_seen desc, id desc
limit sqlc.arg(page)::int;

-- name: SaveFinding :execrows
-- The lifecycle and the assessment, together. Everything a PERSON can change,
-- and nothing a rescan touches — those move in the upsert above, and splitting
-- them is what stops a triage racing a scan into a lost update.
update finding.finding
set state = $3, reason = $4, decided_by = $5, decided_at = $6,
    severity = $7, claimant = $8, actor = $9, confidence = $10,
    basis = $11, assessed_at = $12,
    superseded_severity = $13, superseded_claimant = $14, superseded_actor = $15,
    superseded_confidence = $16, superseded_basis = $17, superseded_at = $18
where id = $1 and workspace_id = $2;

-- name: LiveFindingsPerFragment :many
-- 0009's badge — "this fragment has an open finding" — as a COUNT per fragment,
-- and the worst live severity beside it so a badge can be coloured without a
-- second read.
--
-- It is computed and never materialised on the fragment row: it changes on every
-- scan and on every triage, which is 0037's argument against a materialised
-- coverage grid, one table over.
select fragment_id, count(*)::int as live,
       min(case severity
           when 'critical' then 0 when 'high' then 1 when 'medium' then 2
           when 'low' then 3 else 4 end)::int as worst
from finding.finding
where workspace_id = $1 and state in ('open', 'triaged')
group by fragment_id;

-- name: UpsertDetail :exec
-- ONE ROW PER (finding, field). A nightly rescan UPDATES rather than appends, so
-- this stays bounded while `sightings` grows — the alternative writes a row per
-- field per night forever to record that nothing changed.
insert into finding.detail (
    id, finding_id, field, value, mapping_id, artifact_id, seen_at
) values ($1, $2, $3, $4, $5, $6, $7)
on conflict (finding_id, field) do update
set value = excluded.value, mapping_id = excluded.mapping_id,
    artifact_id = excluded.artifact_id, seen_at = excluded.seen_at
where excluded.seen_at >= finding.detail.seen_at;

-- name: DetailsForFinding :many
select id, finding_id, field, value, mapping_id, artifact_id, seen_at
from finding.detail where finding_id = $1 order by field;

-- name: FindingsForFragments :many
-- Complete report read; an empty set deliberately returns no findings.
select id, workspace_id, tool_id, signature, fragment_id, fragment_kind,
       fragment_value, state, reason, decided_by, decided_at, severity,
       claimant, actor, confidence, basis, assessed_at,
       superseded_severity, superseded_claimant, superseded_actor,
       superseded_confidence, superseded_basis, superseded_at,
       first_seen, last_seen, sightings, invocation_id, artifact_id,
       mapping_id, created_at
from finding.finding
where workspace_id = $1 and fragment_id = any(sqlc.arg(fragments)::uuid[])
order by case severity
    when 'critical' then 0 when 'high' then 1 when 'medium' then 2
    when 'low' then 3 else 4 end,
    last_seen desc, id desc;
