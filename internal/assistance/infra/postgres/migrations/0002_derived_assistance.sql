-- Assistance over extracted text remains tied to the source capture and the
-- exact successful extraction that supplied the provider input.
alter table assistance.operation add column extraction_id uuid;
alter table assistance.proposal add column extraction_id uuid;
create index operation_for_extraction on assistance.operation(workspace_id,source_id,capture_id,extraction_id,created_at desc) where extraction_id is not null;
