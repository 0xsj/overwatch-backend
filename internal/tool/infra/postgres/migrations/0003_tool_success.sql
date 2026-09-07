-- Which exit codes mean success, per tool — decisions/0033.
--
-- pkg/execx says it outright: "nuclei exits 1 when it finds nothing. httpx exits
-- non-zero on an unreachable host. Those are answers." A run that maps every
-- non-zero exit to `failed` collapses FOUND NOTHING into FAILED, which is
-- CLAUDE.md's `never checked vs found nothing` pair breaking one level down.
--
-- REVERSIBLE: one column with a default.

alter table tool.tool
    add column success_exit_codes integer[] not null default '{0}';

-- Empty would mean "no exit code is success", which is a tool that can only ever
-- fail. Nobody wants that and a caller that sends `[]` means `{0}`.
alter table tool.tool add constraint tool_success_present
    check (cardinality(success_exit_codes) > 0);
