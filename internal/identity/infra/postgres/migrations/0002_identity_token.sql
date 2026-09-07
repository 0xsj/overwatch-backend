-- Verification and reset tokens. A fourth table, deliberately late: identity's
-- contract listed it as absent until the flow that needs it existed, and this is
-- that flow.
--
-- IRREVERSIBLE: creates a table. Reversing this is DROP TABLE.

create table identity.token (
    id          uuid        primary key,
    account_id  uuid        not null references identity.account (id) on delete restrict,
    kind        text        not null,
    -- The HASH, never the token. A token is emailed once and a stolen database
    -- must not yield anything presentable — the same rule as a session, for the
    -- same reason and with the same fast hash: 256 random bits have nothing to
    -- guess, so a memory-hard KDF here would only cost the verifier.
    hash        text        not null,
    created_at  timestamptz not null,
    expires_at  timestamptz not null,
    consumed_at timestamptz,

    constraint token_kind_known   check (kind in ('verification', 'password_reset')),
    constraint token_hash_set     check (hash <> ''),
    constraint token_expiry_late  check (expires_at > created_at)
);

-- The lookup on every confirmation: a token is found by the hash of what was
-- presented and by nothing else.
create unique index token_hash_key on identity.token (hash);

-- "Is there a live one of this kind for this account", which is what issuing a
-- new one asks before it invalidates the old. PARTIAL, because a consumed token
-- is history and is never a candidate.
create index token_live on identity.token (account_id, kind)
    where consumed_at is null;
