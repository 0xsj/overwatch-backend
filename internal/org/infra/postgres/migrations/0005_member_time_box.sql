-- THE TIME BOX — owed item D, and decisions/0019 and 0025 both named it.
--
--   0019  "guest and client need a time box, and it is not built here"
--   0025  names it again for invitations
--   0042  gives `client` real access to deliverables
--
-- The third is what made the first two urgent: a client invited for one
-- engagement in January reads that engagement's report in perpetuity, and
-- "time-boxed" was a word in two sealed records and a rule in nothing.
--
-- IRREVERSIBLE: adds a column and a constraint.

alter table org.member add column expires_at timestamptz;

-- EXISTING guest and client rows get a SHORT window, not a plausible one.
--
-- The constraint below would reject them and brick the migration, and there is
-- no honest date to backfill: this migration cannot know what anybody agreed.
-- Thirty days from the deploy is deliberately too short to be mistaken for a
-- decision — the row lands in `member_expiring` immediately, the members screen
-- shows it, and a person sets the real date.
--
-- Inventing `created_at + 1 year` would be worse: it would look like somebody
-- chose it.
update org.member set expires_at = now() + interval '30 days'
where role in ('guest', 'client') and expires_at is null;

-- NON-NULL EXACTLY FOR `guest` AND `client`. An owner, an admin or a member has
-- no end date — they are the firm — and a column that could carry one for them
-- would be a rule enforced nowhere, which is CLAUDE.md §5 in one field.
--
-- Stated as an `=` between two predicates rather than two separate checks,
-- because the two halves are one rule and splitting them is how one gets
-- relaxed without the other.
alter table org.member add constraint member_time_box_matches_role
    check ((role in ('guest', 'client')) = (expires_at is not null));

-- AND ON THE INVITATION, because `0025` said the column arrives with it: an
-- invitation for a time-boxed role has to carry the date, or accepting one
-- would create a membership the membership rule refuses.
--
-- `seat_until` and NOT `expires_at`, which this table already has and which
-- means something else entirely: that one is how long the LINK is good for
-- (seven days, so an unread invitation stops working), and this is how long the
-- PERSON is on the engagement. Confusing them is the mistake the two names exist
-- to prevent.
alter table org.invite add column seat_until timestamptz;

update org.invite set seat_until = now() + interval '30 days'
where role in ('guest', 'client') and seat_until is null and accepted_at is null
  and revoked_at is null;

alter table org.invite add constraint invite_seat_until_matches_role
    check ((role in ('guest', 'client')) = (seat_until is not null));

-- What a SWEEP would read, if there were one. There is not: the gate computes
-- expiry when access is evaluated, because a sweep runs on an interval and
-- between two ticks an expired guest still holds everything they held.
--
-- The index is here anyway because the MEMBERS LIST reads it — a screen showing
-- "expired 3 Jul" beside a seat is the whole point of keeping the row rather
-- than deleting it.
create index member_expiring on org.member (org_id, expires_at)
    where expires_at is not null;
