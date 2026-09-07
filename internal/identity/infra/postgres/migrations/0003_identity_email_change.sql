-- The third token kind, and the address it proposes — decisions/0021.
--
-- REVERSIBLE while no row uses the new kind: dropping the column and narrowing
-- the constraint back is a two-statement undo.

alter table identity.token drop constraint token_kind_known;
alter table identity.token add constraint token_kind_known
    check (kind in ('verification', 'password_reset', 'email_change'));

-- The address the token would move the account TO. It is stored on the token
-- because the caller must not be able to name it at confirmation time: a token
-- that carries its own target lets somebody confirm an address the server never
-- agreed to.
alter table identity.token add column proposed_email text;

-- Set exactly when the kind needs it, and null otherwise. Both halves, because a
-- proposed_email on a password reset is a field nobody reads and a null one on
-- an email_change is a token that cannot do its job — and neither is visible
-- without the constraint that refuses it.
alter table identity.token add constraint token_email_iff_change check (
    (kind =  'email_change' and proposed_email is not null and proposed_email <> '') or
    (kind <> 'email_change' and proposed_email is null)
);

-- Folded, for the same reason identity.account folds its email: two rows
-- differing only by case are two proposals for one address, and the database
-- cannot see that unless the value arrives already lowered. The application
-- normalises in domain.NewEmail; this refuses anything that did not.
alter table identity.token add constraint token_email_folded check (
    proposed_email is null or proposed_email = lower(proposed_email)
);

-- NOT unique. Two people may both propose the same address and at most one can
-- ever confirm it, because confirmation writes identity.account, where
-- account_live_email already refuses the second. Enforcing it here as well would
-- refuse the SECOND PROPOSAL rather than the second confirmation, which tells an
-- unauthenticated-enough caller that an address is spoken for.
