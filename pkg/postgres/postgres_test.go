// Author-written. The reserved-word guard was added on 2026-09-07 after a domain
// called `check` matched the identifier pattern and produced a syntax error in
// DDL, which is a shape check missing a vocabulary problem.
package postgres_test

import (
	"context"
	"testing"

	"github.com/0xsj/overwatch-backend/pkg/errors"
	"github.com/0xsj/overwatch-backend/pkg/postgres"
)

// The pool is nil on purpose: the name is rejected while building the config,
// before anything is dialled. If this ever panics rather than failing, the check
// has moved somewhere it can be reached too late.
func TestAReservedWordIsRefusedAsASchemaName(t *testing.T) {
	for _, name := range []string{"check", "user", "order", "group", "table", "grant"} {
		t.Run(name, func(t *testing.T) {
			_, err := postgres.Migrate(context.Background(), nil, nil, postgres.InSchema(name))
			if !errors.IsKind(err, errors.Invalid) {
				t.Fatalf("%q is reserved SQL and must be refused, got %v", name, err)
			}
		})
	}
}

// THE OVER-REFUSAL CASE IS NOT TESTED HERE, and that is deliberate rather than
// an omission: a legal name proceeds past the config check and dials, so it
// cannot be exercised with a nil pool.
//
// It is covered where it actually matters — pkg/testx runs every domain's schema
// name through this same InSchema on the way to a database, so a guard that
// refused `org` or `identity` would fail the whole e2e suite on its first
// migration rather than pass quietly.
