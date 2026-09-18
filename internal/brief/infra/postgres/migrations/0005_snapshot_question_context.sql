alter table brief.snapshot_question add column observation_ids jsonb not null default '[]'::jsonb;
