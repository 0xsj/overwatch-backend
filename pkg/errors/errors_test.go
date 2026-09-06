// AUTHOR-WRITTEN. Not produced behind an information barrier: written by someone
// who had read the implementation, and in most cases the surviving mutants that
// exposed the gap. Separate from spec_test.go so provenance is a property of the
// file rather than of a comment block somebody has to notice.
//
// custody/ records which run each of these came from.
package errors_test

import (
	"testing"

	"github.com/0xsj/overwatch-backend/pkg/errors"
)

// POST-HOC, 2026-09-06. An independently written spec-derived suite
// (custody/0004) tested DetailsOf against a two-level chain and passed with the
// historical double-advance bug restored — the bug is invisible at two levels.
// The spec now says "every *Error in the chain"; this is the three-level shape
// that distinguishes it.
func TestDetailsOfVisitsEveryLevelNotJustTheEnds(t *testing.T) {
	inner := errors.New(errors.Conflict, "inner").WithDetail("c", "3")
	mid := errors.Wrap(inner, errors.Invalid, "mid").WithDetail("b", "2")
	outer := errors.Wrap(mid, errors.Internal, "outer").WithDetail("a", "1")

	got := errors.DetailsOf(outer)
	for _, k := range []string{"a", "b", "c"} {
		if _, ok := got[k]; !ok {
			t.Errorf("detail %q was dropped from %v — a walk that advances twice per iteration collects the ends and steps over the middle, and two levels cannot tell the difference", k, got)
		}
	}
}
