-- identity's schema. Every constraint here is a rule the code above already
-- states; this is the half that still holds when something bypasses it.
--
-- IRREVERSIBLE: creates a schema and three tables. There is no down migration
-- and reversing this is DROP SCHEMA.
--
-- The schema itself is created by postgres.Migrate (see InSchema), because the
-- ledger is written before this file runs. The line below is a no-op that
-- restates where these tables live, for a reader who opens this file alone.

create schema if not exists identity;

-- ---------------------------------------------------------------- account ---

create table identity.account (
    id         uuid        primary key,
    email      text        not null,
    name       text        not null default '',
    status     text        not null,
    version    integer     not null,
    created_at timestamptz not null,
    updated_at timestamptz not null,

    constraint account_email_length check (length(email) between 3 and 254),
    -- NewEmail folds case once, on the way in. This index only means what it
    -- appears to mean while that holds, and a hand-written row cannot break it.
    constraint account_email_lower  check (email = lower(email)),
    constraint account_name_length  check (length(name) <= 120),
    constraint account_status_known check (status in ('pending', 'active', 'archived')),
    constraint account_version_min  check (version >= 1)
);

-- PARTIAL, and the predicate is the decision. An archived account keeps its
-- email forever because every attribution it authored still names the row, and
-- decisions/0012 says restoring access is creating an account rather than
-- reviving one. A total unique index would therefore make an address
-- unusable for life the first time somebody left.
create unique index account_live_email on identity.account (email)
    where status <> 'archived';

-- ------------------------------------------------------------- credential ---

create table identity.credential (
    id         uuid        primary key,
    account_id uuid        not null references identity.account (id) on delete restrict,
    kind       text        not null,
    hash       text        not null,
    name       text        not null default '',
    expires_at timestamptz,
    revoked_at timestamptz,
    version    integer     not null,
    created_at timestamptz not null,
    updated_at timestamptz not null,

    constraint credential_kind_known  check (kind in ('password', 'api_key')),
    constraint credential_hash_set    check (hash <> ''),
    constraint credential_name_length check (length(name) <= 60),
    -- an api key nobody can name is an api key nobody can revoke
    constraint credential_key_named   check (kind <> 'api_key' or name <> ''),
    constraint credential_version_min check (version >= 1),
    constraint credential_expiry_late check (expires_at is null or expires_at > created_at)
);

create unique index credential_hash_key on identity.credential (hash);

-- One live password per account, and PARTIAL for the same reason as the email:
-- a revoked password stays on the row that recorded the change. Without this
-- the domain's "one per account, replacing it is an update" is a convention
-- that a retry can violate.
create unique index credential_one_live_password
    on identity.credential (account_id)
    where kind = 'password' and revoked_at is null;

create index credential_account on identity.credential (account_id, created_at desc);

-- ---------------------------------------------------------------- session ---

create table identity.session (
    id         uuid        primary key,
    account_id uuid        not null references identity.account (id) on delete restrict,
    hash       text        not null,
    user_agent text        not null default '',
    address    text        not null default '',
    issued_at  timestamptz not null,
    expires_at timestamptz not null,
    revoked_at timestamptz,

    constraint session_hash_set     check (hash <> ''),
    constraint session_expiry_late  check (expires_at > issued_at),
    constraint session_agent_length check (length(user_agent) <= 512),
    constraint session_address_len  check (length(address) <= 45)
);

-- The lookup on every authenticated request. A session is found by the hash of
-- the token presented and by nothing else.
create unique index session_hash_key on identity.session (hash);

-- "show me my sessions", newest first, and the sweep that ends them all when a
-- credential changes.
create index session_account on identity.session (account_id, issued_at desc);
