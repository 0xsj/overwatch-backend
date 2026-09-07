-- The rule that PERMITTED a spawn — decisions/0035's lineage, step four.
--
-- `refusal_rule` recorded the rule that refused. Nothing recorded the rule that
-- ALLOWED, so `PRODUCT.md`'s sentence — "every value walks backwards to […] the
-- scope rule that allowed the command to run" — resolved to nothing on every
-- successful invocation, which is all of them. Found by walking a real lineage.
--
-- TWO COLUMNS AND NOT ONE. A refusal and a permission are different facts and
-- `0010` keeps them apart everywhere else: exclude beats include, `Refuse` and
-- `NotInScope` are separate constructors, and a refusal with no rule means
-- nothing permitted it. Collapsing them into `scope_rule` would need the phase
-- to disambiguate, which is a join on a column that already exists.
--
-- REVERSIBLE: one nullable column.

alter table run.invocation add column permit_rule uuid;

-- A permitting rule may only appear where a process was permitted — the mirror
-- of `invocation_rule_only_on_refusal`. `pending` is included because the plan
-- writes the rule BEFORE anything spawns, which is the whole of 0033 §1.
alter table run.invocation add constraint invocation_permit_not_on_refusal check (
    permit_rule is null or phase <> 'refused'
);

-- What "which spawns did this rule permit" reads. It is the other half of
-- `invocation_refusal`, and together they are why 0030 keeps a superseded rule.
create index invocation_permit on run.invocation (permit_rule)
    where permit_rule is not null;
