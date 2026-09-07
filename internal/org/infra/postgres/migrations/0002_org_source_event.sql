-- decisions/0017: an org is provisioned by a subscriber, and delivery is
-- at-least-once. The event that caused a row is recorded so a redelivery is a
-- no-op rather than a second org.
--
-- NULLABLE, because an org created by a person has no causing event, and a
-- PARTIAL unique index so those rows do not collide with each other on NULL.
-- This is the pattern audit.entry and journal.line already use; the difference
-- is only that theirs is not nullable, because everything they hold arrived on
-- an event.

alter table org.org add column if not exists source_event_id uuid;

create unique index if not exists org_source_event
    on org.org (source_event_id) where source_event_id is not null;
