alter table research.connection drop constraint connection_kind_check;
alter table research.connection add constraint connection_kind_check check (kind in ('associated_with','may_belong_to','mentions','concerns_same_event','located_at','possible_same_subject'));

alter table research.connection_revision drop constraint connection_revision_kind_check;
alter table research.connection_revision add constraint connection_revision_kind_check check (kind in ('associated_with','may_belong_to','mentions','concerns_same_event','located_at','possible_same_subject'));
