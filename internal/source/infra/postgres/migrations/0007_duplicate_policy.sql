alter table source.source
  add column duplicate_policy text not null default 'warn'
  check (duplicate_policy in ('allow', 'warn', 'block'));
