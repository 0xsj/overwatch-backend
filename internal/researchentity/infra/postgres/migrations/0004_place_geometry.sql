create table research.record_place_geometry (
    workspace_id   uuid not null,
    record_id      uuid primary key references research.record(id) on delete cascade,
    latitude       double precision not null check (latitude between -90 and 90),
    longitude      double precision not null check (longitude between -180 and 180),
    precision      text not null check (precision in ('exact','approximate','region')),
    observation_ids jsonb not null default '[]'::jsonb check (jsonb_typeof(observation_ids) = 'array'),
    updated_by     uuid not null,
    updated_at     timestamptz not null
);

create index record_place_geometry_for_workspace on research.record_place_geometry(workspace_id, record_id);
