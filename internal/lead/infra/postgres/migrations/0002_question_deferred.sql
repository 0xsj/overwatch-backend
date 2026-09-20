alter table lead.question drop constraint if exists question_state_check;
alter table lead.question add constraint question_state_check
    check (state in ('open','answered','dismissed','deferred'));

alter table lead.question drop constraint if exists question_check;
alter table lead.question add constraint question_check
    check (
      (state = 'open' and resolution = '')
      or (state in ('answered','dismissed','deferred') and resolution <> '')
    );
