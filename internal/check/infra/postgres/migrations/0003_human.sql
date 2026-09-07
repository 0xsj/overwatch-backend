-- A check declares that it is HUMAN — decisions/0037 §3.
--
-- It was going to be derived from an empty chain, because `0032` called a
-- chainless check the human one. The first live coverage grid showed why that is
-- wrong within a minute: a check somebody created and has not wired a chain to
-- yet is ALSO chainless, and it reported every asset `fresh` the moment one was
-- read.
--
--   human      nothing spawns, and a PERSON reading it is the whole act
--   unfinished nothing spawns YET, and it has never been run
--
-- Two different things sharing a shape. A chainless non-human check reports
-- `never` forever, which is true — it cannot run.
--
-- REVERSIBLE: one column with a default.

alter table checks.check add column human boolean not null default false;

-- A human check has no chain, and that direction of the implication is still
-- true — it is only the converse that was wrong. Nothing enforces it here
-- because a chain is a different table and a constraint across two is a trigger.
