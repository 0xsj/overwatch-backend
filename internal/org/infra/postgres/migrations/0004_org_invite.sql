-- Invitations — decisions/0025.
--
-- IRREVERSIBLE: creates a table. Reversing it is DROP TABLE.

create table org.invite (
    id           uuid        primary key,
    org_id       uuid        not null references org.org (id) on delete restrict,

    -- The address the INVITER TYPED. It is not a copy of an account's email —
    -- the invitee may have no account at all — which is what keeps this a fact
    -- about the invitation rather than a denormalisation of identity's table.
    -- It is folded, because accepting compares it to the caller's address and
    -- two spellings of one address would be two different invitations.
    email        text        not null,
    role         text        not null,

    -- Who sent it, and (optionally) the first engagement they are for.
    -- decisions/0025: an invitation with no grant drops somebody into an org
    -- where they can see nothing, so the inviter names the workspace at the
    -- moment they know which one it is.
    invited_by   uuid        not null,
    workspace_id uuid,
    level        text,

    -- The HASH, never the token. Same rule as every other link this system
    -- sends, and the same fast hash: 256 random bits have nothing to guess.
    hash         text        not null,

    created_at   timestamptz not null,
    expires_at   timestamptz not null,
    accepted_at  timestamptz,
    accepted_by  uuid,
    revoked_at   timestamptz,

    constraint invite_email_present check (email <> '' and email = lower(email)),
    constraint invite_role_known    check (role in ('owner', 'admin', 'member', 'guest', 'client')),
    constraint invite_hash_set      check (hash <> ''),
    constraint invite_expiry_late   check (expires_at > created_at),

    -- A workspace and a level arrive together or not at all. One without the
    -- other is an invitation that cannot say what access it confers.
    constraint invite_grant_paired check (
        (workspace_id is null and level is null) or
        (workspace_id is not null and level is not null)
    ),
    -- `none` is not a storable level anywhere in this schema — a grant of none
    -- is a deletion — so an invitation cannot carry one either.
    constraint invite_level_known check (level is null or level in ('read', 'write', 'admin')),

    -- accepted_at and accepted_by are one fact in two columns and must agree.
    constraint invite_accepted_paired check (
        (accepted_at is null and accepted_by is null) or
        (accepted_at is not null and accepted_by is not null)
    ),
    -- An invitation cannot be both taken and withdrawn.
    constraint invite_not_both check (accepted_at is null or revoked_at is null)
);

create unique index invite_hash_key on org.invite (hash);

-- PARTIAL, and the predicate is the decision — decisions/0025. ONE LIVE
-- INVITATION per address per org: sending a second consumes the first, for the
-- same reason issuing a verification link consumes the outstanding one. Two live
-- links means the older still works after somebody asked for a newer.
--
-- Accepted and revoked rows stay forever: "who was invited, by whom, and did
-- they take it" is the question this table exists to answer.
create unique index invite_live on org.invite (org_id, email)
    where accepted_at is null and revoked_at is null;

create index invite_org on org.invite (org_id, created_at desc);
