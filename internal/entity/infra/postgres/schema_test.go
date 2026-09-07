// Author-written. decisions/0009 accepted a known cost and named it:
//
//	"Two identical column groups will drift. One will gain a field the other
//	 does not, and nothing checks it. The alternative was a polymorphic FK the
//	 database could not enforce at all, so the trade is a drift that A SCHEMA
//	 DIFF CAN CATCH against an invariant that nothing could."
//
// This is that schema diff. It is the same move `root/vocabulary_test.go` makes
// for the kind vocabulary: duplication is the contract only if something checks
// it.
package postgres_test

import (
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"
)

const migration = "migrations/0001_entity.sql"

// judgementColumn captures the suffix and the TYPE WORD — the pair that can
// drift. It deliberately stops at the type: `not null default 'unopened'` is a
// default, not a shape, and a group that gained one on one side is a smaller
// problem than one that gained a column.
//
// The first draft required whitespace after the type and so matched only
// `judgement_state`, because `uuid,` and `text,` end in a comma. It compared one
// column against one column and passed — a check that agrees by matching
// nothing, which is CLAUDE.md §4's exact failure.
var judgementColumn = regexp.MustCompile(`(?m)^\s+judgement_(\w+)\s+(\w+)`)

func table(t *testing.T, sql, name string) string {
	t.Helper()
	open := "create table entity." + name + " ("
	from := strings.Index(sql, open)
	if from < 0 {
		t.Fatalf("no table %q in %s", name, migration)
	}
	rest := sql[from:]
	end := strings.Index(rest, "\n);")
	if end < 0 {
		t.Fatalf("table %q is not terminated", name)
	}
	return rest[:end]
}

// columns is the part of a table body BEFORE the first constraint. Splitting
// there matters: a constraint body mentions `judgement_state` too, and the first
// draft of this test matched those and compared nonsense that happened to be
// equal on one side and not the other.
func columns(body string) string {
	if at := strings.Index(body, "constraint "); at >= 0 {
		return body[:at]
	}
	return body
}

func judgementShape(t *testing.T, body string) []string {
	t.Helper()
	out := make([]string, 0, 4)
	for _, m := range judgementColumn.FindAllStringSubmatch(columns(body), -1) {
		out = append(out, m[1]+" "+strings.TrimSpace(m[2]))
	}
	sort.Strings(out)
	return out
}

// judgementConstraint matches the constraint NAMES, minus the table prefix, so
// `entity_judgement_ruled` and `fragment_judgement_ruled` compare equal.
var judgementConstraint = regexp.MustCompile(`constraint (?:entity|fragment)_(judgement_\w+)`)

func judgementRules(body string) []string {
	out := make([]string, 0, 4)
	for _, m := range judgementConstraint.FindAllStringSubmatch(body, -1) {
		out = append(out, m[1])
	}
	sort.Strings(out)
	return out
}

// THE TEST 0009 ASKED FOR. A field added to one group and not the other, or a
// constraint added to one and not the other, fails here — which is the only
// place it can fail, because Postgres cannot express "these two column groups
// are the same thing".
func TestTheTwoJudgementColumnGroupsHaveTheSameShape(t *testing.T) {
	raw, err := os.ReadFile(migration)
	if err != nil {
		t.Fatal(err)
	}
	sql := string(raw)

	onEntity := judgementShape(t, table(t, sql, "entity"))
	onFragment := judgementShape(t, table(t, sql, "fragment"))
	if len(onEntity) != 4 {
		t.Fatalf("expected four judgement columns on entity, got %v", onEntity)
	}
	same(t, "columns", onEntity, onFragment)

	same(t, "constraints",
		judgementRules(table(t, sql, "entity")),
		judgementRules(table(t, sql, "fragment")))
}

func same(t *testing.T, what string, a, b []string) {
	t.Helper()
	if len(a) != len(b) {
		t.Fatalf("judgement %s differ:\n  entity   %v\n  fragment %v", what, a, b)
	}
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("judgement %s differ:\n  entity   %v\n  fragment %v", what, a, b)
		}
	}
}
