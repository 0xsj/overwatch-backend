alter table assistance.proposal add column relationship_kind text not null default '' check (relationship_kind in ('','associated_with','may_belong_to','mentions','concerns_same_event','located_at','possible_same_subject'));
alter table assistance.proposal add column related_candidate_kind text not null default '' check (related_candidate_kind in ('','person','account','organisation','place'));
alter table assistance.proposal add column related_candidate_name text not null default '' check (octet_length(related_candidate_name) <= 400);
alter table assistance.proposal add column relationship_description text not null default '' check (octet_length(relationship_description) <= 4000);
