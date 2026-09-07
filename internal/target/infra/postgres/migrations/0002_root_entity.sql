-- The column decisions/0029 declared and left always-zero.
--
--   "RootEntity is the node representing this target in the graph, and is
--    ALWAYS ZERO today — `entity` does not exist, and the column arrives in the
--    migration that creates the row it points at."
--
-- `entity` exists now (0036). A subscriber on `target.added` creates the root,
-- and a second on `entity.root.created` writes it here — two subscribers and no
-- cross-schema write, the shape 0007 and 0020 already established.
--
-- REVERSIBLE: one nullable column.

alter table target.target add column root_entity_id uuid;

-- One target per root, and the index is the rule rather than a lookup: a
-- redelivered `entity.root.created` must not be able to point two targets at one
-- entity.
create unique index target_one_root on target.target (root_entity_id)
    where root_entity_id is not null;
