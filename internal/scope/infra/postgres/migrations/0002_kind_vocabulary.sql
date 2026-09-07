-- The kinds a rule may name, constrained by the database for the first time —
-- decisions/0034.
--
-- The column has always been text[] with no name check: the domain refused an
-- unknown kind and nothing else did. That was survivable while `scope` was the
-- only writer; it is not now that two other domains hold copies of this list.
--
-- REVERSIBLE: drops to a plain text[].

alter table scope.rule add constraint rule_kinds_known check (
    kinds <@ array[
        'host', 'cidr', 'ip', 'asn', 'url',
        'repo', 'email', 'account', 'document', 'org', 'person',
        'cert', 'key', 'whois'
    ]::text[]
);

-- The SPAWN GATE takes a disjoint subset — 0010, widened by 0034 to admit `asn`
-- and `url` because asnmap and whois take an ASN and nuclei takes a URL.
--
-- Stated here as well as in the domain because 0010's disjointness is the whole
-- reason the two gates are one table, and a constraint is what keeps a direct
-- INSERT from doing what the domain refuses.
alter table scope.rule add constraint rule_spawn_kinds_are_spawnable check (
    gate <> 'spawn' or
    kinds <@ array['host', 'cidr', 'ip', 'asn', 'url']::text[]
);
