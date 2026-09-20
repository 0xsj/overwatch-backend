alter table timeline.event_participant
    add column role text not null default 'associated'
    check (role in ('associated','actor','subject','target','witness','affected','reporter'));

alter table timeline.event_revision
    add column participant_links jsonb not null default '[]'::jsonb;
