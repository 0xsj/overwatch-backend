-- applies_to is the TARGETABLE FACET, not a vocabulary of its own — 0034.
--
-- Coverage is over ASSETS, and 0009 defines an asset as a fragment whose kind is
-- targetable. So this is seven of scope's fourteen, and `root` holds a test that
-- fails the moment it is not.
--
-- IRREVERSIBLE: `domain` is folded into `host` and the two cannot be told apart
-- afterwards.
--
-- The schema is `checks` and not `check` — `check` is the SQL constraint keyword.
-- This file said `"check".check` in its first draft, which is the exact mistake
-- the rename was made to stop being possible to make.

-- A check that applied to both `domain` and `host` would otherwise end up with
-- `host` twice, so the rewrite goes through a de-duplicating aggregate rather
-- than array_replace.
update checks.check
set applies_to = (
    select array_agg(distinct case when k = 'domain' then 'host' else k end)
    from unnest(applies_to) as k
)
where 'domain' = any(applies_to);

alter table checks.check drop constraint check_applies_known;

alter table checks.check add constraint check_applies_known check (
    applies_to <@ array['host', 'cidr', 'ip', 'asn', 'url', 'repo', 'email']::text[]
);
