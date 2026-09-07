-- One kind vocabulary — decisions/0034.
--
-- `domain` is deleted: a domain is a HOST IN A ROLE, which is 0009's argument
-- about assets applied one level down. Rows carrying it are rewritten rather
-- than refused, because `acme.com` really was a host all along and there is
-- nothing to ask anybody.
--
-- IRREVERSIBLE IN ONE DIRECTION: the update is lossless going forward and cannot
-- distinguish a row that said `host` from one that said `domain` going back.
-- That is the point of the decision.

update tool.tool set consumes = 'host' where consumes = 'domain';
update tool.tool set produces = 'host' where produces = 'domain';

alter table tool.tool drop constraint tool_feed_known;

-- The vocabulary plus `finding`. A finding is a CLAUDE.md §Scope noun with its
-- own lifecycle and is explicitly NOT an observation — so it is not a fragment,
-- and it is a thing bytes can be between two programs.
alter table tool.tool add constraint tool_feed_known check (
    (consumes is null or consumes in (
        'host', 'cidr', 'ip', 'asn', 'url',
        'repo', 'email', 'account', 'document', 'org', 'person',
        'cert', 'key', 'whois', 'finding')) and
    (produces is null or produces in (
        'host', 'cidr', 'ip', 'asn', 'url',
        'repo', 'email', 'account', 'document', 'org', 'person',
        'cert', 'key', 'whois', 'finding'))
);
