alter table research.connection_revision add column from_record_kind text not null default '';
alter table research.connection_revision add column from_record_name text not null default '';
alter table research.connection_revision add column to_record_kind text not null default '';
alter table research.connection_revision add column to_record_name text not null default '';
