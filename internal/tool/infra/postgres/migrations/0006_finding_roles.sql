-- The two roles a FINDING needs — decisions/0041 §2.
--
--   signature   what the tool calls this class of problem. Half a finding's
--               identity, and the reason a rescan is a sighting rather than a
--               new row
--   severity    the tool's own assessment. It arrives as a `rule` claimant
--               with NO confidence — 0004: a nuclei template asserting `high`
--               is a category, not a probability, and storing 1.0 destroys the
--               distinction permanently
--
-- The role vocabulary is five a DAY after it became three, which is worth
-- noticing rather than waving through. The test each one passed is whether it
-- names a distinct QUESTION about a field. A sixth added for a screen rather
-- than for a question is the one to refuse.
--
-- IRREVERSIBLE: widens a check constraint.

alter table tool.mapping drop constraint mapping_role_known;

alter table tool.mapping add constraint mapping_role_known
    check (role in ('subject', 'attribute', 'derived_from', 'signature', 'severity'));

-- ONE LIVE SIGNATURE and one live severity per tool, for the same reason there
-- is one live subject: two answers to "what does this tool call this problem"
-- is no answer, and the identity of every finding would depend on which row was
-- read first.
create unique index mapping_one_signature on tool.mapping (tool_id)
    where role = 'signature' and state = 'live';

create unique index mapping_one_severity on tool.mapping (tool_id)
    where role = 'severity' and state = 'live';
