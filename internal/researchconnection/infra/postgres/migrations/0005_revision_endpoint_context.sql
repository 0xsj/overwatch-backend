alter table research.connection_revision add column from_record_description text not null default '';
alter table research.connection_revision add column from_record_observation_ids jsonb not null default '[]'::jsonb;
alter table research.connection_revision add column to_record_description text not null default '';
alter table research.connection_revision add column to_record_observation_ids jsonb not null default '[]'::jsonb;
