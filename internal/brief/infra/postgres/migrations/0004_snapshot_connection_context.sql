alter table brief.snapshot_connection add column from_record_description text not null default '';
alter table brief.snapshot_connection add column from_record_observation_ids jsonb not null default '[]'::jsonb;
alter table brief.snapshot_connection add column to_record_description text not null default '';
alter table brief.snapshot_connection add column to_record_observation_ids jsonb not null default '[]'::jsonb;
