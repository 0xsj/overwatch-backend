alter table assistance.proposal add column candidate_kind text not null default '' check (candidate_kind in ('','person','account','organisation','place'));
alter table assistance.proposal add column candidate_name text not null default '' check (octet_length(candidate_name) <= 400);
alter table assistance.proposal add column candidate_description text not null default '' check (octet_length(candidate_description) <= 4000);
