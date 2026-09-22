-- 0011 created an inline check named alert_check for source-backed alerts.
-- Derived question/record/cluster alerts deliberately have no source, so the
-- newer source_alert_kind_shape constraint is authoritative.
alter table source.alert drop constraint if exists alert_check;
