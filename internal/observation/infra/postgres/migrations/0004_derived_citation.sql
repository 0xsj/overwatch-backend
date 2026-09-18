-- A manual observation may cite the exact text emitted by one immutable
-- extraction attempt while retaining the original binary capture identity.
alter table observation.manual add column extraction_id uuid;
create index manual_for_extraction on observation.manual(workspace_id,source_id,extraction_id,id desc) where extraction_id is not null;
