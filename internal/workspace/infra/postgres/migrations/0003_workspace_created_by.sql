-- Who started this engagement — decisions/0020.
--
-- REVERSIBLE: this adds one nullable column. Dropping it loses the record and
-- nothing else.

alter table workspace.workspace add column created_by uuid;

-- NULLABLE, and it stays nullable. Three reasons, in order of importance:
--
-- 1 · NULL means "never recorded", not "nobody". Every row written before this
--     migration was provisioned by the registration chain and the person who
--     caused it is knowable only from the journal — which is EXPIRABLE, so for
--     an old enough row the honest answer is that we do not know. Backfilling a
--     guess would turn "never checked" into "found nothing", which CLAUDE.md
--     lists among the pairs that must never collapse.
--
-- 2 · The only way to backfill it is to read org.member, and a cross-schema read
--     inside a migration is worse than a nullable column: it would make this
--     file fail against a database that has no org schema, which is exactly the
--     extraction decisions/0017 says a dump of one schema should permit.
--
-- 3 · A NOT NULL with a sentinel is the same lie with extra steps.
--
-- Everything written from here on sets it, and the domain refuses to CONSTRUCT a
-- workspace without one. Loading an old row is a different operation from
-- creating a new one, and only the second can insist.

-- It is deliberately NOT indexed. "Every workspace this person started" is not a
-- screen, and an index on a column nothing filters by is a write cost with no
-- reader. Add it when a query needs it.

-- It names identity's row and is NOT a foreign key, for the reason every other
-- cross-schema id here is not one — decisions/0017. And it is never consulted by
-- the authorisation gate: who may manage a workspace is the admin grant plus the
-- owner exemption (decisions/0019), and a second source for that question is a
-- second source that can disagree.
