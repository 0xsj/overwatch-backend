alter table timeline.event_relationship drop constraint if exists event_relationship_kind_check;
alter table timeline.event_relationship add constraint event_relationship_kind_check
    check (kind in ('related','precedes','overlaps','same_occurrence_candidate','possibly_causes'));
