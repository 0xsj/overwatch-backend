package errors_test

import (
	"context"
	stderrors "errors"
	"fmt"
	"strings"
	"testing"

	"github.com/0xsj/overwatch-backend/pkg/errors"
)

// declaredKinds transcribes the const block from the package's documented
// surface. Kinds is checked against this rather than derived from it: a kind
// that reaches one list and not the other is the three-list drift the notes
// call the most valuable thing in kind.go to guard.
var declaredKinds = []errors.Kind{
	errors.Internal,
	errors.Invalid,
	errors.NotFound,
	errors.Conflict,
	errors.Unauthenticated,
	errors.Forbidden,
	errors.RateLimited,
	errors.Unavailable,
	errors.Timeout,
	errors.Canceled,
	errors.Unprocessable,
	errors.PreconditionFailed,
	errors.PreconditionRequired,
}

// leak stands in for everything Err is allowed to carry and Msg is not.
const leak = `pq: password authentication failed for user "admin"`

func counted(ks []errors.Kind) map[errors.Kind]int {
	m := make(map[errors.Kind]int, len(ks))
	for _, k := range ks {
		m[k]++
	}
	return m
}

func sameSet(a, b []errors.Kind) bool {
	as, bs := counted(a), counted(b)
	if len(as) != len(bs) {
		return false
	}
	for k := range as {
		if _, ok := bs[k]; !ok {
			return false
		}
	}
	return true
}

func hasKind(ks []errors.Kind, want errors.Kind) bool {
	for _, k := range ks {
		if k == want {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Kind: the closed set
// ---------------------------------------------------------------------------

func TestKindIsAClosedSet(t *testing.T) {
	t.Run("Kinds lists exactly the constants the package declares", func(t *testing.T) {
		if !sameSet(errors.Kinds, declaredKinds) {
			t.Errorf("Kinds = %v, declared const block = %v; a kind missing from Kinds is silently skipped by every totality test built on it, so the list stops being an asset and becomes a promise", errors.Kinds, declaredKinds)
		}
	})

	t.Run("no kind is listed twice", func(t *testing.T) {
		for k, n := range counted(errors.Kinds) {
			if n != 1 {
				t.Errorf("kind %q appears %d times in Kinds; a list that repeats a member cannot be used to prove a transport mapping is total", k, n)
			}
		}
	})

	t.Run("every listed kind has a name rather than the numeric fallback", func(t *testing.T) {
		for _, k := range errors.Kinds {
			name := k.String()
			if name == "" {
				t.Errorf("kind %d renders as the empty string; String() lands in log fields, dashboards and stored records, where an empty value is unqueryable", uint8(k))
			}
			if name == fmt.Sprintf("kind(%d)", uint8(k)) {
				t.Errorf("kind %d renders as its numeric fallback %q; a kind present in Kinds but absent from the name table is exactly the drift between the three lists that nothing but a test can catch", uint8(k), name)
			}
		}
	})

	t.Run("no two kinds share a name", func(t *testing.T) {
		seen := make(map[string]errors.Kind, len(errors.Kinds))
		for _, k := range errors.Kinds {
			if prev, ok := seen[k.String()]; ok {
				t.Errorf("kinds %d and %d both render as %q; the name is the persisted form, so two kinds sharing one makes a stored dead-letter record ambiguous on the way back in", uint8(prev), uint8(k), k.String())
			}
			seen[k.String()] = k
		}
	})
}

func TestKindZeroValueFailsClosed(t *testing.T) {
	t.Run("an unassigned Kind is Internal", func(t *testing.T) {
		var k errors.Kind
		if k != errors.Internal {
			t.Errorf("the zero Kind is %q, want Internal; a field nobody assigned must read as a 500 that is loudly ours, never as a plausible-looking 404 or a 400 blaming the caller for our defect", k)
		}
	})

	t.Run("an Error built without a constructor classifies as Internal", func(t *testing.T) {
		if got := errors.KindOf(&errors.Error{}); got != errors.Internal {
			t.Errorf("a struct-literal Error classifies as %q, want Internal; the safe default has to hold by construction, not by remembering to pass a kind", got)
		}
	})

	t.Run("an Error with no message and no cause still renders something", func(t *testing.T) {
		got := (&errors.Error{}).Error()
		if got == "" {
			t.Error("an Error with no Msg and no cause rendered as the empty string; an error that prints as nothing is invisible exactly when someone is reading a log to find it")
		}
		if got != errors.Internal.String() {
			t.Errorf("an Error with no Msg and no cause rendered %q, want the kind name %q; falling back to the kind is what makes an unfilled error honest and greppable", got, errors.Internal.String())
		}
	})
}

func TestParseKindRoundTripsThroughTheName(t *testing.T) {
	t.Run("every kind parses back from its own rendering", func(t *testing.T) {
		for _, k := range errors.Kinds {
			got, ok := errors.ParseKind(k.String())
			if !ok {
				t.Errorf("ParseKind(%q) reported not-ok; the name is the only persisted form, so a kind that cannot be read back is write-only and a stored record naming it is unreplayable", k.String())
				continue
			}
			if got != k {
				t.Errorf("ParseKind(%q) = %q, want %q; round-tripping through the name is precisely what makes reordering the const block safe", k.String(), got, k)
			}
		}
	})

	t.Run("name to kind to name is stable for every kind", func(t *testing.T) {
		for _, k := range errors.Kinds {
			again, ok := errors.ParseKind(k.String())
			if !ok || again.String() != k.String() {
				t.Errorf("%q round-tripped to %q (ok=%v); the numeric value is deliberately never persisted, so the name has to survive the trip intact", k.String(), again.String(), ok)
			}
		}
	})
}

func TestParseKindDoesNotInventKinds(t *testing.T) {
	cases := []struct {
		name  string
		input string
	}{
		{"the empty string must not match an unnamed gap in the name table", ""},
		{"a name nobody declared is not a kind", "quarantined"},
		{"the rendering of an unnamed kind must not parse back into one", errors.Kind(200).String()},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := errors.ParseKind(c.input)
			if ok {
				t.Errorf("ParseKind(%q) = %q, ok; accepting an input that names no kind lets a corrupt stored value come back as a legitimate classification, which is a silent wrong answer at the moment it costs most", c.input, got)
			}
			if got != errors.Internal {
				t.Errorf("ParseKind(%q) failed but returned %q; a failed parse must fail closed on Internal for the same reason the zero value does", c.input, got)
			}
		})
	}
}

func TestKindStringForAnUnnamedValueIsDiagnosable(t *testing.T) {
	t.Run("an unnamed kind names the const that is missing a name", func(t *testing.T) {
		got := errors.Kind(200).String()
		if got != "kind(200)" {
			t.Errorf("Kind(200).String() = %q, want \"kind(200)\"; \"unknown\" says something broke, kind(200) says exactly which const is missing an entry in the name table", got)
		}
	})
}

// ---------------------------------------------------------------------------
// Retryability
// ---------------------------------------------------------------------------

func TestRetryabilityIsAPropertyOfTheKind(t *testing.T) {
	t.Run("the verdict for a kind does not change between calls", func(t *testing.T) {
		for _, k := range errors.Kinds {
			if k.Retryable() != k.Retryable() {
				t.Errorf("%q.Retryable() disagreed with itself; a verdict recoverable from a kind read out of a log must not depend on when it was asked", k)
			}
		}
	})

	t.Run("a batch of one retries exactly as its kind does", func(t *testing.T) {
		for _, k := range errors.Kinds {
			got, want := errors.Retryable(errors.New(k, "something failed")), k.Retryable()
			if got != want {
				t.Errorf("Retryable(New(%q, ...)) = %v but %q.Retryable() = %v; the batch question and the class question may differ on a mixed batch by design, but on a batch of one a divergence means a caller cannot reason about either", k, got, k, want)
			}
		}
	})

	t.Run("annotating with fmt.Errorf does not change the retry verdict", func(t *testing.T) {
		for _, k := range errors.Kinds {
			err := fmt.Errorf("register source: %w", errors.New(k, "something failed"))
			got, want := errors.Retryable(err), k.Retryable()
			if got != want {
				t.Errorf("annotation changed the retry verdict for %q from %v to %v; fmt.Errorf adds a layer carrying no Error, so it must add context and never reclassify — a layer that silently reclassifies is how a transient failure becomes permanent", k, want, got)
			}
		}
	})
}

func TestDocumentedRetryVerdicts(t *testing.T) {
	cases := []struct {
		name string
		kind errors.Kind
		want bool
	}{
		{"a deadline is retryable because a per-attempt budget expiring is what retries are for", errors.Timeout, true},
		{"a cancellation is not retryable because the caller has already gone", errors.Canceled, false},
		{"an unavailable dependency is retryable, which is what keeps a wrapped repository timeout retryable through two layers", errors.Unavailable, true},
		{"an invalid input is permanent, so an identical retry fails identically", errors.Invalid, false},
		// Inferred, not stated outright: the reaction RateLimited names is
		// "slow down", and the notes say it usually arrives with a Retry-After,
		// which is only meaningful if the caller comes back.
		{"a rate limit is retryable because the reaction it names is slow down and come back", errors.RateLimited, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.kind.Retryable(); got != c.want {
				t.Errorf("%q.Retryable() = %v, want %v: %s", c.kind, got, c.want, c.name)
			}
		})
	}
}

func TestCancellationIsNotRetryable(t *testing.T) {
	t.Run("the Canceled kind is not retryable", func(t *testing.T) {
		if errors.Canceled.Retryable() {
			t.Error("Canceled must not be retryable: a wave of client disconnects retried as failures spends work on callers who have gone and trips a breaker on a dependency that was never unhealthy")
		}
	})

	t.Run("an error classified as a cancellation is not retryable", func(t *testing.T) {
		if errors.Retryable(errors.New(errors.Canceled, "drain in progress")) {
			t.Error("a cancelled unit of work must not be retried: cancellation is not a failure of the dependency, so re-running it is pure waste")
		}
	})

	// The doc says context errors "enter the vocabulary" in this package and
	// that Canceled is not retryable. Whether Retryable classifies a bare
	// context error the way KindOf does is not stated outright; a fail-closed
	// reading says it must.
	t.Run("a bare context cancellation is not retryable", func(t *testing.T) {
		if errors.Retryable(context.Canceled) {
			t.Error("a bare context.Canceled must not report retryable: retry logic branches on this predicate without knowing context exists, so an unclassified cancellation reported as retryable retries every drained request")
		}
	})
}

// ---------------------------------------------------------------------------
// Construction
// ---------------------------------------------------------------------------

func TestConstructorsNeverReturnNil(t *testing.T) {
	cases := []struct {
		name string
		got  *errors.Error
	}{
		{"New yields a value", errors.New(errors.NotFound, "source not found")},
		{"New yields a value even with an empty message", errors.New(errors.Internal, "")},
		{"Newf yields a value", errors.Newf(errors.Invalid, "field %s", "email")},
		{"Wrap yields a value", errors.Wrap(stderrors.New("boom"), errors.Unavailable, "feed unreachable")},
		{"Wrap of a nil cause still yields a value", errors.Wrap(nil, errors.Unavailable, "feed unreachable")},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if c.got == nil {
				t.Fatal("a constructor returning a nil *Error puts a typed nil in every error interface it is assigned to, and an interface holding (type, nil) is not nil — so every if err != nil upstream fires on success")
			}
			if c.got.Error() == "" {
				t.Error("a constructed error rendered as nothing at all; there is no state of this type in which an empty rendering is the honest answer")
			}
		})
	}

	t.Run("wrapping a nil cause still carries the kind it was given", func(t *testing.T) {
		if got := errors.KindOf(errors.Wrap(nil, errors.Unavailable, "feed unreachable")); got != errors.Unavailable {
			t.Errorf("Wrap(nil, Unavailable, ...) classified as %q; returning *Error means no constructor may return nil, so Wrap of nothing has to be a real error and it has to keep the classification asked for", got)
		}
	})
}

func TestNewRecordsWhatItWasGiven(t *testing.T) {
	e := errors.New(errors.NotFound, "source not found")

	t.Run("the kind is the one asked for", func(t *testing.T) {
		if e.Kind != errors.NotFound {
			t.Errorf("Kind = %q, want NotFound; choosing the kind once, where the rule lives, is the whole point of the sentinel pattern", e.Kind)
		}
	})
	t.Run("the caller-safe message is the one asked for", func(t *testing.T) {
		if e.Msg != "source not found" {
			t.Errorf("Msg = %q, want \"source not found\"", e.Msg)
		}
	})
	t.Run("a freshly constructed error carries no cause", func(t *testing.T) {
		if e.Err != nil {
			t.Errorf("Err = %v, want nil; New states a failure, it does not translate one", e.Err)
		}
	})
	t.Run("a freshly constructed error carries no slug", func(t *testing.T) {
		if got := errors.TypeOf(e); got != "" {
			t.Errorf("TypeOf = %q, want empty; no slug is the right answer for a failure whose Kind is all there is to say", got)
		}
	})
	t.Run("a freshly constructed error carries no fields or details", func(t *testing.T) {
		if n := len(errors.FieldsOf(e)); n != 0 {
			t.Errorf("FieldsOf returned %d entries for an error nobody attached a field to", n)
		}
		if n := len(errors.DetailsOf(e)); n != 0 {
			t.Errorf("DetailsOf returned %d entries for an error nobody attached a detail to", n)
		}
	})
}

func TestNewfFormatsTheCallerSafeMessage(t *testing.T) {
	t.Run("the formatted message is what a caller is shown", func(t *testing.T) {
		e := errors.Newf(errors.Invalid, "field %s must be at least %d characters", "name", 3)
		want := "field name must be at least 3 characters"
		if e.Msg != want {
			t.Errorf("Msg = %q, want %q", e.Msg, want)
		}
		if got := errors.Message(e); got != want {
			t.Errorf("Message = %q, want %q; whatever is interpolated into Newf ends up caller-visible, so the formatted result must be the caller-safe message and nothing else", got, want)
		}
	})

	t.Run("Newf is New over a formatted message for every kind", func(t *testing.T) {
		for _, k := range errors.Kinds {
			f := errors.Newf(k, "cannot %s %d sources", "list", 7)
			n := errors.New(k, "cannot list 7 sources")
			if f.Error() != n.Error() {
				t.Errorf("Newf and New diverged for kind %q: %q vs %q; a second constructor that formats differently is a second vocabulary to keep in sync", k, f.Error(), n.Error())
			}
			if f.Kind != n.Kind {
				t.Errorf("Newf and New disagreed on the kind for %q", k)
			}
		}
	})
}

func TestErrorRenderingIsTotal(t *testing.T) {
	cause := stderrors.New("pq: dial timeout")
	t.Run("no combination of kind, message and cause renders as nothing", func(t *testing.T) {
		for _, k := range errors.Kinds {
			for _, e := range []*errors.Error{
				errors.New(k, ""),
				errors.New(k, "list sources"),
				errors.Wrap(cause, k, ""),
				errors.Wrap(cause, k, "list sources"),
			} {
				if e.Error() == "" {
					t.Errorf("an Error of kind %q rendered as the empty string; the rendering is what the edge logs once, so there is no state in which saying nothing is correct", k)
				}
			}
		}
	})
}

func TestErrorRenderingComposesTheChain(t *testing.T) {
	cause := stderrors.New("pq: dial timeout")
	cases := []struct {
		name string
		err  *errors.Error
		want string
	}{
		{"no message and no cause falls back to the kind name", errors.New(errors.Internal, ""), errors.Internal.String()},
		{"a message and no cause renders the message alone", errors.New(errors.NotFound, "source not found"), "source not found"},
		{"a cause and no message renders the cause alone", errors.Wrap(cause, errors.Unavailable, ""), "pq: dial timeout"},
		{"a message and a cause compose with a colon", errors.Wrap(cause, errors.Unavailable, "list sources"), "list sources: pq: dial timeout"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.err.Error(); got != c.want {
				t.Errorf("Error() = %q, want %q; messages are lowercase, unpunctuated and colon-joined so the whole chain reads as one sentence in a log", got, c.want)
			}
		})
	}
}

func TestErrorUnwrapExposesTheCause(t *testing.T) {
	cause := stderrors.New("boom")

	t.Run("Wrap keeps the cause reachable", func(t *testing.T) {
		if got := errors.Wrap(cause, errors.Conflict, "source already registered").Unwrap(); got != cause {
			t.Errorf("Unwrap = %v, want the wrapped cause; a translation that severs the chain throws away the only diagnosis of what actually went wrong", got)
		}
	})

	t.Run("an error with no cause unwraps to nothing", func(t *testing.T) {
		if got := errors.New(errors.NotFound, "source not found").Unwrap(); got != nil {
			t.Errorf("Unwrap = %v, want nil; New states a failure rather than translating one, so there is nothing beneath it", got)
		}
	})

	t.Run("the package Unwrap agrees with the method", func(t *testing.T) {
		e := errors.Wrap(cause, errors.Conflict, "source already registered")
		if got := errors.Unwrap(e); got != cause {
			t.Errorf("errors.Unwrap = %v, want the wrapped cause; nothing in the stdlib shim may ever gain logic, so the two paths must be indistinguishable", got)
		}
	})
}

// ---------------------------------------------------------------------------
// Classification: outermost wins, annotation preserves
// ---------------------------------------------------------------------------

func TestTheOutermostErrorWins(t *testing.T) {
	t.Run("Wrap reclassifies whatever the inner layer decided", func(t *testing.T) {
		for _, inner := range errors.Kinds {
			for _, outer := range errors.Kinds {
				err := errors.Wrap(errors.New(inner, "inner"), outer, "outer")
				if got := errors.KindOf(err); got != outer {
					t.Errorf("Wrap of %q as %q classified as %q; the outermost Error has to win, or infrastructure cannot translate a foreign error without reaching into what the domain already decided", inner, outer, got)
				}
			}
		}
	})

	t.Run("a reclassification survives an annotation above it", func(t *testing.T) {
		for _, inner := range errors.Kinds {
			for _, outer := range errors.Kinds {
				err := fmt.Errorf("register source: %w", errors.Wrap(errors.New(inner, "inner"), outer, "outer"))
				if got := errors.KindOf(err); got != outer {
					t.Errorf("annotating a %q reclassification reported %q; fmt.Errorf adds a layer carrying no Error, so it must leave the classification exactly where it was", outer, got)
				}
			}
		}
	})

	t.Run("an Error's own kind wins over a context cause beneath it", func(t *testing.T) {
		err := errors.Wrap(context.Canceled, errors.Unavailable, "feed unreachable")
		if got := errors.KindOf(err); got != errors.Unavailable {
			t.Errorf("KindOf = %q, want Unavailable; context errors are classified only where nothing else has classified the failure, otherwise an adapter's deliberate translation would be overruled from underneath", got)
		}
	})
}

func TestAnnotationDoesNotReclassify(t *testing.T) {
	t.Run("one annotation layer preserves the kind", func(t *testing.T) {
		for _, k := range errors.Kinds {
			err := fmt.Errorf("register source: %w", errors.New(k, "inner"))
			if got := errors.KindOf(err); got != k {
				t.Errorf("one fmt.Errorf layer turned %q into %q; a layer that restates the kind and guesses wrong silently reclassifies, which is how a transient failure becomes permanent", k, got)
			}
		}
	})

	t.Run("three annotation layers preserve the kind", func(t *testing.T) {
		for _, k := range errors.Kinds {
			err := fmt.Errorf("handle request: %w", fmt.Errorf("list sources: %w", fmt.Errorf("query sources: %w", errors.New(k, "inner"))))
			if got := errors.KindOf(err); got != k {
				t.Errorf("three fmt.Errorf layers turned %q into %q; depth of annotation must not erode a classification, or the by-layer rule that app code annotates is unsafe to follow", k, got)
			}
		}
	})
}

func TestAnnotationNeverRemovesInformation(t *testing.T) {
	sentinel := errors.New(errors.Conflict, "source already registered").
		WithType("source-exists").
		WithField("name", "already taken").
		WithDetail("constraint", "source_name_key")

	layered := fmt.Errorf("register source: %w", fmt.Errorf("insert source: %w", sentinel))

	t.Run("the kind survives annotation", func(t *testing.T) {
		if got := errors.KindOf(layered); got != errors.Conflict {
			t.Errorf("KindOf = %q, want Conflict; adding a layer that carries no Error must never remove what an inner layer knew", got)
		}
	})
	t.Run("the type slug survives annotation", func(t *testing.T) {
		if got := errors.TypeOf(layered); got != "source-exists" {
			t.Errorf("TypeOf = %q, want \"source-exists\"; the slug is how a client tells apart failures a status code cannot, and an annotation must not cost it that", got)
		}
	})
	t.Run("the caller-facing fields survive annotation", func(t *testing.T) {
		if got := errors.FieldsOf(layered)["name"]; got != "already taken" {
			t.Errorf("FieldsOf[\"name\"] = %q, want \"already taken\"; validation answers with every problem at once, and an annotation between the validator and the edge must not eat them", got)
		}
	})
	t.Run("the log-only details survive annotation", func(t *testing.T) {
		if got := errors.DetailsOf(layered)["constraint"]; got != "source_name_key" {
			t.Errorf("DetailsOf[\"constraint\"] = %q, want \"source_name_key\"; a detail is attached where the fact is known and the layer above does not know it, so wrapping must not discard it", got)
		}
	})
	t.Run("sentinel identity survives annotation", func(t *testing.T) {
		if !errors.Is(layered, sentinel) {
			t.Error("Is could not find the sentinel through two annotation layers; sentinels are ordinary values compared with Is, and that identity is the only error vocabulary a domain publishes")
		}
	})
	t.Run("the retry verdict survives annotation", func(t *testing.T) {
		if errors.Retryable(layered) != errors.Retryable(sentinel) {
			t.Error("annotation changed the retry verdict; in a pipeline this predicate decides whether work is retried or dead-lettered, and adding context to a message is not a reason to change that")
		}
	})
	t.Run("the caller-safe message survives annotation", func(t *testing.T) {
		if got, want := errors.Message(layered), errors.Message(sentinel); got != want {
			t.Errorf("Message = %q, want %q; the annotation is for the log chain, not for the caller, so it must not alter what the caller is told", got, want)
		}
	})
}

func TestSentinelIdentitySurvivesTranslation(t *testing.T) {
	sentinel := errors.New(errors.Conflict, "source already registered")

	t.Run("identity survives an annotation layer", func(t *testing.T) {
		if !errors.Is(fmt.Errorf("insert source: %w", sentinel), sentinel) {
			t.Error("Is failed through one fmt.Errorf; the adapter pattern for a recognised foreign error is to wrap the sentinel so identity and Kind both survive")
		}
	})

	t.Run("identity survives a join with unrelated failures", func(t *testing.T) {
		batch := errors.Join(stderrors.New("boom"), sentinel, stderrors.New("other"))
		if !errors.Is(batch, sentinel) {
			t.Error("Is failed inside a joined batch; partial failure is the normal case here, so a batch that hides which known condition occurred is a batch nobody can act on")
		}
	})

	t.Run("Wrap reclassifies without severing identity", func(t *testing.T) {
		reclassified := errors.Wrap(sentinel, errors.Internal, "translated")
		if !errors.Is(reclassified, sentinel) {
			t.Error("Wrap severed the chain; reclassification is meant to change how a caller reacts, not to erase what happened")
		}
		if got := errors.KindOf(reclassified); got != errors.Internal {
			t.Errorf("KindOf = %q, want Internal; the outermost Error wins, which is the entire reason Wrap exists", got)
		}
	})
}

// ---------------------------------------------------------------------------
// Copy-on-write builders
// ---------------------------------------------------------------------------

func TestBuildersAreCopyOnWrite(t *testing.T) {
	newBase := func() *errors.Error {
		return errors.New(errors.Invalid, "invalid source").
			WithField("email", "required").
			WithDetail("constraint", "source_email_key")
	}

	t.Run("WithType does not give the value it derives from a slug", func(t *testing.T) {
		base := newBase()
		derived := base.WithType("source-invalid")
		if got := errors.TypeOf(base); got != "" {
			t.Errorf("the receiver gained the slug %q; builders are called on shared package sentinels, so a mutation would outlive the request that caused it", got)
		}
		if got := errors.TypeOf(derived); got != "source-invalid" {
			t.Errorf("the derived error has slug %q, want \"source-invalid\"", got)
		}
	})

	t.Run("WithType does not share the Fields map with the value it derives from", func(t *testing.T) {
		base := newBase()
		derived := base.WithType("source-invalid")
		if derived.Fields == nil {
			t.Fatal("the derived error lost the fields it was built from; attaching a slug must not remove a caller-facing problem")
		}
		derived.Fields["email"] = "MUTATED"
		derived.Fields["injected"] = "MUTATED"

		got := errors.FieldsOf(base)
		if got["email"] != "required" {
			t.Errorf("the receiver's field became %q; a struct copy shares the map header, so a derived error and the sentinel it came from would mutate together", got["email"])
		}
		if _, ok := got["injected"]; ok {
			t.Error("a key injected into the derived error appeared on the receiver; the aliasing this rules out was a real bug, not a hypothetical one")
		}
	})

	t.Run("WithType does not share the Details map with the value it derives from", func(t *testing.T) {
		base := newBase()
		derived := base.WithType("source-invalid")
		if derived.Details == nil {
			t.Fatal("the derived error lost the details it was built from; a detail is attached where the fact is known and nothing above knows it well enough to re-attach it")
		}
		derived.Details["constraint"] = "MUTATED"
		derived.Details["injected"] = "MUTATED"

		got := errors.DetailsOf(base)
		if got["constraint"] != "source_email_key" {
			t.Errorf("the receiver's detail became %q; clone copies both maps precisely so WithDetail on a shared error is as safe as WithField always was", got["constraint"])
		}
		if _, ok := got["injected"]; ok {
			t.Error("a key injected into the derived error's details appeared on the receiver")
		}
	})

	t.Run("WithField does not add the field to the value it derives from", func(t *testing.T) {
		base := newBase()
		derived := base.WithField("name", "required")
		if _, ok := errors.FieldsOf(base)["name"]; ok {
			t.Error("the receiver gained the derived error's field; package sentinels are shared values and a builder that mutates one poisons every later use of it")
		}
		if got := errors.FieldsOf(derived)["name"]; got != "required" {
			t.Errorf("the derived error's field is %q, want \"required\"", got)
		}
	})

	t.Run("WithDetail does not add the detail to the value it derives from", func(t *testing.T) {
		base := newBase()
		derived := base.WithDetail("subject", "account:1234")
		if _, ok := errors.DetailsOf(base)["subject"]; ok {
			t.Error("the receiver gained the derived error's detail; a diagnostic naming a subject leaking onto a shared sentinel would attach one request's subject to every later failure")
		}
		if got := errors.DetailsOf(derived)["subject"]; got != "account:1234" {
			t.Errorf("the derived error's detail is %q, want \"account:1234\"", got)
		}
	})

	// Not stated outright by the spec: whether the builder copies the map the
	// caller hands it, as opposed to the map already on the receiver. It is the
	// same aliasing hazard from the other side.
	t.Run("WithFields does not alias the map the caller handed it", func(t *testing.T) {
		caller := map[string]string{"email": "required"}
		e := errors.New(errors.Invalid, "invalid source").WithFields(caller)
		caller["email"] = "MUTATED"
		caller["injected"] = "MUTATED"

		got := errors.FieldsOf(e)
		if got["email"] != "required" {
			t.Errorf("the error's field followed the caller's map to %q; an error is a value that outlives the scope that built it, so it must not keep a window into that scope's mutable state", got["email"])
		}
		if _, ok := got["injected"]; ok {
			t.Error("a key added to the caller's map after construction appeared on the error")
		}
	})

	t.Run("WithDetails does not alias the map the caller handed it", func(t *testing.T) {
		caller := map[string]string{"constraint": "source_email_key"}
		e := errors.New(errors.Conflict, "source already registered").WithDetails(caller)
		caller["constraint"] = "MUTATED"
		caller["injected"] = "MUTATED"

		got := errors.DetailsOf(e)
		if got["constraint"] != "source_email_key" {
			t.Errorf("the error's detail followed the caller's map to %q", got["constraint"])
		}
		if _, ok := got["injected"]; ok {
			t.Error("a key added to the caller's map after construction appeared on the error's details")
		}
	})

	t.Run("a builder preserves the kind, message and cause it was called on", func(t *testing.T) {
		cause := stderrors.New("boom")
		base := errors.Wrap(cause, errors.Unprocessable, "cannot process source")
		for name, derived := range map[string]*errors.Error{
			"WithType":    base.WithType("slug"),
			"WithField":   base.WithField("a", "b"),
			"WithFields":  base.WithFields(map[string]string{"a": "b"}),
			"WithDetail":  base.WithDetail("a", "b"),
			"WithDetails": base.WithDetails(map[string]string{"a": "b"}),
		} {
			if derived == nil {
				t.Fatalf("%s returned nil; a builder returning a nil *Error puts a typed nil into every error interface downstream", name)
			}
			if derived.Kind != errors.Unprocessable {
				t.Errorf("%s changed the kind to %q; only Wrap reclassifies, and a builder that quietly does is the reclassification bug with no call site to blame", name, derived.Kind)
			}
			if derived.Msg != "cannot process source" {
				t.Errorf("%s changed the caller-safe message to %q", name, derived.Msg)
			}
			if derived.Err != cause {
				t.Errorf("%s changed the cause to %v; losing it loses the only diagnosis", name, derived.Err)
			}
		}
	})
}

// ---------------------------------------------------------------------------
// Fields and Details: two audiences
// ---------------------------------------------------------------------------

func TestFieldsOfReturnsACopy(t *testing.T) {
	e := errors.New(errors.Invalid, "invalid source").WithField("email", "required")

	t.Run("mutating the returned map does not reach the error", func(t *testing.T) {
		got := errors.FieldsOf(e)
		if got == nil {
			t.Fatal("FieldsOf returned nil for an error carrying a field")
		}
		got["email"] = "MUTATED"
		got["injected"] = "MUTATED"

		again := errors.FieldsOf(e)
		if again["email"] != "required" {
			t.Errorf("the error's field became %q; package sentinels are shared values, and handing a caller a reference into one is a bug written once and found a year later", again["email"])
		}
		if _, ok := again["injected"]; ok {
			t.Error("a key injected into the returned map appeared on the error itself")
		}
	})
}

func TestDetailsOfReturnsACopy(t *testing.T) {
	e := errors.New(errors.Conflict, "source already registered").WithDetail("constraint", "source_name_key")

	t.Run("mutating the returned map does not reach the error", func(t *testing.T) {
		got := errors.DetailsOf(e)
		if got == nil {
			t.Fatal("DetailsOf returned nil for an error carrying a detail")
		}
		got["constraint"] = "MUTATED"
		got["injected"] = "MUTATED"

		again := errors.DetailsOf(e)
		if again["constraint"] != "source_name_key" {
			t.Errorf("the error's detail became %q; the same aliasing argument that makes FieldsOf copy applies here, and a log line rewritten by a reader is worse than no log line", again["constraint"])
		}
		if _, ok := again["injected"]; ok {
			t.Error("a key injected into the returned map appeared on the error itself")
		}
	})
}

func TestFieldsOfStopsAtTheOutermostError(t *testing.T) {
	inner := errors.New(errors.Invalid, "invalid source").WithField("email", "required")

	t.Run("a reclassifying wrap does not republish the inner error's fields", func(t *testing.T) {
		outer := errors.Wrap(inner, errors.Conflict, "source already registered")
		if _, ok := errors.FieldsOf(outer)["email"]; ok {
			t.Error("FieldsOf reached past the outermost Error; a field is attached by whoever produced the caller-facing error, so an inner error's fields are the answer to a question the caller did not ask")
		}
	})

	t.Run("an annotation layer does not hide the fields", func(t *testing.T) {
		if got := errors.FieldsOf(fmt.Errorf("register source: %w", inner))["email"]; got != "required" {
			t.Errorf("FieldsOf = %q through one annotation, want \"required\"; fmt.Errorf carries no Error of its own, so the outermost Error is still the inner one", got)
		}
	})
}

func TestDetailsOfWalksTheWholeChain(t *testing.T) {
	inner := errors.New(errors.Conflict, "source already registered").WithDetail("constraint", "source_name_key")

	t.Run("a detail attached below a wrap is not discarded", func(t *testing.T) {
		outer := errors.Wrap(inner, errors.Conflict, "conflict").WithDetail("op", "insert source")
		got := errors.DetailsOf(outer)
		if got["constraint"] != "source_name_key" {
			t.Errorf("DetailsOf[\"constraint\"] = %q; pkg/postgres knows the constraint name and the command wrapping it does not, so stopping at the outermost would let one Wrap silently discard the only identifying fact about a conflict", got["constraint"])
		}
		if got["op"] != "insert source" {
			t.Errorf("DetailsOf[\"op\"] = %q, want \"insert source\"; the walk must collect the whole chain, not just its deepest link", got["op"])
		}
	})

	t.Run("an inner key wins a collision because it is the more specific claim", func(t *testing.T) {
		outer := errors.Wrap(inner, errors.Conflict, "conflict").WithDetail("constraint", "GUESSED_BY_AN_OUTER_LAYER")
		if got := errors.DetailsOf(outer)["constraint"]; got != "source_name_key" {
			t.Errorf("DetailsOf[\"constraint\"] = %q, want \"source_name_key\"; the layer closest to the failure knows most about it, so an outer layer's guess must not overwrite it", got)
		}
	})

	// "along the whole chain" is the promise; whether the walk survives a layer
	// that is not an *Error is where the spec stops being explicit.
	t.Run("a detail below an annotation layer is not discarded", func(t *testing.T) {
		outer := errors.Wrap(fmt.Errorf("insert source: %w", inner), errors.Conflict, "conflict")
		if got := errors.DetailsOf(outer)["constraint"]; got != "source_name_key" {
			t.Errorf("DetailsOf[\"constraint\"] = %q; the by-layer rule tells app code to annotate with fmt.Errorf, so a walk that stops at the first non-Error layer discards details in exactly the arrangement the package asks for", got)
		}
	})

	t.Run("monotonicity: wrapping never shrinks the detail set", func(t *testing.T) {
		before := errors.DetailsOf(inner)
		after := errors.DetailsOf(errors.Wrap(inner, errors.Internal, "translated"))
		for k, v := range before {
			if after[k] != v {
				t.Errorf("detail %q was %q before wrapping and %q after; adding a layer must never remove information", k, v, after[k])
			}
		}
	})
}

func TestFieldsAndDetailsHaveDifferentAudiences(t *testing.T) {
	e := errors.New(errors.Forbidden, "forbidden").
		WithField("role", "insufficient").
		WithDetail("reason", "no role grants this action").
		WithDetail("subject", "account:1234")

	t.Run("a detail never appears among the caller-facing fields", func(t *testing.T) {
		fields := errors.FieldsOf(e)
		for k, v := range errors.DetailsOf(e) {
			if k == "role" {
				continue
			}
			if got, ok := fields[k]; ok {
				t.Errorf("detail %q surfaced in FieldsOf as %q; transport marshals FieldsOf into the response body, and a 403 that names the subject and the reason tells an attacker exactly what to acquire", k, got)
			}
			for fk, fv := range fields {
				if fv == v {
					t.Errorf("detail %q leaked into field %q as %q; the split exists because the rule has to be stated at the producer rather than inferred downstream", k, fk, fv)
				}
			}
		}
	})

	t.Run("a detail never reaches the caller-safe message", func(t *testing.T) {
		msg := errors.Message(e)
		for k, v := range errors.DetailsOf(e) {
			if strings.Contains(msg, v) {
				t.Errorf("Message contains the detail %q (%q); WithDetail attaches a diagnostic that must never reach a caller, and Message is the string a caller is shown", k, v)
			}
		}
	})

	t.Run("a field is still readable by the caller", func(t *testing.T) {
		if got := errors.FieldsOf(e)["role"]; got != "insufficient" {
			t.Errorf("FieldsOf[\"role\"] = %q, want \"insufficient\"; the whole point of the split is that fields stay usable, not that both bags go quiet", got)
		}
	})
}

// ---------------------------------------------------------------------------
// Message: the leak guard
// ---------------------------------------------------------------------------

func TestMessageNeverSurfacesTheCause(t *testing.T) {
	t.Run("no kind lets the wrapped cause into the caller-safe message", func(t *testing.T) {
		for _, k := range errors.Kinds {
			err := errors.Wrap(stderrors.New(leak), k, "source not found")
			if strings.Contains(errors.Message(err), leak) {
				t.Errorf("Message leaked the cause for kind %q; Err carries SQL text, feed URLs with keys in the query string and upstream response bodies, and an upstream failure never leaks an upstream body", k)
			}
		}
	})

	t.Run("the full chain is still there for the log to render", func(t *testing.T) {
		for _, k := range errors.Kinds {
			err := errors.Wrap(stderrors.New(leak), k, "source not found")
			if !strings.Contains(err.Error(), leak) {
				t.Errorf("Error() dropped the cause for kind %q; the two states have to be distinguishable — the edge logs the full chain once and shows the caller only Message", k)
			}
		}
	})
}

func TestMessageCollapsesToTheGenericString(t *testing.T) {
	generic := errors.Message(nil)

	t.Run("the generic string is not itself empty", func(t *testing.T) {
		if generic == "" {
			t.Error("Message(nil) is the empty string; an empty caller-safe message is a slip, and the fail-closed reading of a slip is to say something generic rather than nothing at all")
		}
	})

	cases := []struct {
		name string
		err  error
	}{
		{"nil is not an error anyone may be told about", nil},
		{"an Internal error hides even its own message", errors.New(errors.Internal, "connection string rejected")},
		{"an Internal error hides its message however it was built", errors.Wrap(stderrors.New(leak), errors.Internal, "connection string rejected")},
		{"an empty caller-safe message is a slip, and a slip says nothing specific", errors.New(errors.NotFound, "")},
		{"a foreign error carries no caller-safe message at all", stderrors.New(leak)},
		{"an annotated foreign error carries none either", fmt.Errorf("query sources: %w", stderrors.New(leak))},
		{"a joined batch of foreign errors carries none either", errors.Join(stderrors.New(leak), stderrors.New("boom"))},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := errors.Message(c.err)
			if got != generic {
				t.Errorf("Message = %q, want the generic string %q; every case with nothing safe to say has to collapse to the same answer, or the answer itself tells a caller which case they hit", got, generic)
			}
			if strings.Contains(got, leak) {
				t.Errorf("Message = %q and contains the internal cause", got)
			}
			if strings.Contains(got, "connection string rejected") {
				t.Errorf("Message = %q and contains a message that was only ever safe for a non-Internal kind; Message additionally hides Msg for Internal because an Internal failure's message is written for us, not for a caller", got)
			}
		})
	}
}

func TestMessageGenericStringIsPinned(t *testing.T) {
	t.Run("the generic string is the one the package documents", func(t *testing.T) {
		if got := errors.Message(nil); got != "internal error" {
			t.Errorf("Message(nil) = %q, want \"internal error\"; the exact wording is arbitrary but it reaches callers, so it is pinned rather than left to drift", got)
		}
	})
}

func TestMessageIsTheSafeHalfOfTheContract(t *testing.T) {
	t.Run("a non-Internal error shows its own message", func(t *testing.T) {
		for _, k := range errors.Kinds {
			if k == errors.Internal {
				continue
			}
			err := errors.Wrap(stderrors.New(leak), k, "source not found")
			if got := errors.Message(err); got != "source not found" {
				t.Errorf("Message for kind %q = %q, want \"source not found\"; Msg exists to be shown, and a guard that hides everything makes the caller-safe half of the contract worthless", k, got)
			}
		}
	})
}

// ---------------------------------------------------------------------------
// KindOf, IsKind
// ---------------------------------------------------------------------------

func TestKindOfFailsSafe(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want errors.Kind
	}{
		{"nil is not recognised, and an unrecognised error must be a 500", nil, errors.Internal},
		{"a foreign error is not recognised", stderrors.New("boom"), errors.Internal},
		{"an annotated foreign error is still not recognised", fmt.Errorf("query sources: %w", stderrors.New("boom")), errors.Internal},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := errors.KindOf(c.err); got != c.want {
				t.Errorf("KindOf = %q, want %q; an unset or foreign error has to become a 500 rather than leak through as a 200", got, c.want)
			}
		})
	}
}

func TestKindOfClassifiesContextErrors(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want errors.Kind
	}{
		{"a cancelled context enters the vocabulary as Canceled", context.Canceled, errors.Canceled},
		{"an expired deadline enters the vocabulary as Timeout", context.DeadlineExceeded, errors.Timeout},
		{"an annotated cancellation still classifies", fmt.Errorf("collect: %w", context.Canceled), errors.Canceled},
		{"an annotated deadline still classifies", fmt.Errorf("collect: %w", context.DeadlineExceeded), errors.Timeout},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := errors.KindOf(c.err); got != c.want {
				t.Errorf("KindOf = %q, want %q; classifying context errors here is what lets retry and breaker logic branch on Retryable without either module knowing that context exists", got, c.want)
			}
		})
	}

	t.Run("a deadline and a cancellation stay distinguishable", func(t *testing.T) {
		if errors.KindOf(context.Canceled) == errors.KindOf(context.DeadlineExceeded) {
			t.Error("a cancellation and a deadline classified the same; a timeout is retryable and a cancellation is not, and collapsing them makes a drain look like an unhealthy dependency")
		}
	})
}

func TestIsKindIsFalseForNil(t *testing.T) {
	t.Run("nil is an error of no kind at all", func(t *testing.T) {
		for _, k := range errors.Kinds {
			if errors.IsKind(nil, k) {
				t.Errorf("IsKind(nil, %q) reported true; the predicate exists so a missing error cannot satisfy an assertion about one, which is how deleting a validation left every one of ten subtests green", k)
			}
		}
	})

	t.Run("the mapping and the predicate deliberately disagree on nil", func(t *testing.T) {
		if errors.KindOf(nil) != errors.Internal {
			t.Errorf("KindOf(nil) = %q, want Internal; the mapping fails safe so a missed nil check reveals nothing rather than panicking at the edge", errors.KindOf(nil))
		}
		if errors.IsKind(nil, errors.Internal) {
			t.Error("IsKind(nil, Internal) reported true; conflating the safe mapping with the honest predicate is the exact bug the split was made to make unwriteable")
		}
	})
}

func TestIsKindAgreesWithKindOfForRealErrors(t *testing.T) {
	t.Run("a classified error answers true for its own kind and false for every other", func(t *testing.T) {
		for _, actual := range errors.Kinds {
			err := errors.New(actual, "something failed")
			for _, probe := range errors.Kinds {
				want := probe == actual
				if got := errors.IsKind(err, probe); got != want {
					t.Errorf("IsKind(New(%q), %q) = %v, want %v; a predicate that does not agree with the classification it reads is a second vocabulary nobody asked for", actual, probe, got, want)
				}
			}
		}
	})

	t.Run("the agreement survives annotation", func(t *testing.T) {
		for _, actual := range errors.Kinds {
			err := fmt.Errorf("register source: %w", errors.New(actual, "something failed"))
			if !errors.IsKind(err, actual) {
				t.Errorf("IsKind lost %q through one annotation layer; the by-layer rule tells app code to annotate, so a predicate that cannot see through an annotation is unusable above the domain", actual)
			}
		}
	})

	// Ambiguous in the spec: "IsKind is the predicate. It is false for nil" names
	// nil as the only divergence from KindOf, which reads as IsKind(err, k) ==
	// (err != nil && KindOf(err) == k). A predicate keyed on finding an *Error
	// instead would answer false here — and would also answer false for a bare
	// context.Canceled probed as Canceled, which the doc gives no licence for.
	t.Run("a foreign error is Internal to the predicate as well as to the mapping", func(t *testing.T) {
		foreign := stderrors.New("boom")
		if errors.KindOf(foreign) != errors.Internal {
			t.Errorf("KindOf on a foreign error = %q, want Internal", errors.KindOf(foreign))
		}
		if !errors.IsKind(foreign, errors.Internal) {
			t.Error("IsKind(foreign, Internal) reported false; a foreign error really is an internal fault as far as a caller is concerned, and only nil is the case the predicate exists to rule out")
		}
	})
}

// ---------------------------------------------------------------------------
// Batches
// ---------------------------------------------------------------------------

func TestKindsOfIsOrderIndependent(t *testing.T) {
	t.Run("swapping two branches does not change the set of kinds found", func(t *testing.T) {
		for _, a := range errors.Kinds {
			for _, b := range errors.Kinds {
				ab := errors.KindsOf(errors.Join(errors.New(a, "a"), errors.New(b, "b")))
				ba := errors.KindsOf(errors.Join(errors.New(b, "b"), errors.New(a, "a")))
				if !sameSet(ab, ba) {
					t.Errorf("join(%q, %q) found %v but join(%q, %q) found %v; the batch questions are the order-independent ones, and a set that depends on argument order is not a set", a, b, ab, b, a, ba)
				}
			}
		}
	})

	t.Run("every branch of a batch contributes its kind", func(t *testing.T) {
		for _, a := range errors.Kinds {
			for _, b := range errors.Kinds {
				got := errors.KindsOf(errors.Join(errors.New(a, "a"), errors.New(b, "b")))
				if !hasKind(got, a) || !hasKind(got, b) {
					t.Errorf("join(%q, %q) found %v; collapsing a mixed batch to fewer kinds throws away the only information the caller needs", a, b, got)
				}
			}
		}
	})

	t.Run("a three-way batch reports all three however it was ordered", func(t *testing.T) {
		want := []errors.Kind{errors.Invalid, errors.RateLimited, errors.Timeout}
		forward := errors.KindsOf(errors.Join(
			errors.New(errors.Invalid, "malformed record"),
			errors.New(errors.RateLimited, "slow down"),
			errors.New(errors.Timeout, "upstream slow"),
		))
		reverse := errors.KindsOf(errors.Join(
			errors.New(errors.Timeout, "upstream slow"),
			errors.New(errors.RateLimited, "slow down"),
			errors.New(errors.Invalid, "malformed record"),
		))
		if !sameSet(forward, want) {
			t.Errorf("forward order found %v, want the set %v; a fifty-item poll yielding one malformed record, one rate limit and one timeout is Tuesday, not an edge case", forward, want)
		}
		if !sameSet(reverse, want) {
			t.Errorf("reverse order found %v, want the set %v", reverse, want)
		}
	})
}

func TestKindsOfReachesJoinsNestedUnderAnnotation(t *testing.T) {
	t.Run("a batch annotated by its caller still reports every kind", func(t *testing.T) {
		batch := errors.Join(
			errors.New(errors.Invalid, "malformed record"),
			errors.New(errors.RateLimited, "slow down"),
			errors.New(errors.Timeout, "upstream slow"),
		)
		got := errors.KindsOf(fmt.Errorf("poll feed: %w", batch))
		if len(got) == 0 {
			t.Fatal("a walk following only the single-Unwrap interface finds nothing under a join, and against a total transport mapping no kind becomes a 500 — a classified batch reported as a server error")
		}
		for _, want := range []errors.Kind{errors.Invalid, errors.RateLimited, errors.Timeout} {
			if !hasKind(got, want) {
				t.Errorf("%q missing from %v; the walk has to handle Unwrap() []error, not just Unwrap() error", want, got)
			}
		}
	})

	t.Run("a join nested inside another join is still reached", func(t *testing.T) {
		inner := errors.Join(errors.New(errors.Invalid, "a"), errors.New(errors.Timeout, "b"))
		got := errors.KindsOf(errors.Join(inner, errors.New(errors.Conflict, "c")))
		for _, want := range []errors.Kind{errors.Invalid, errors.Timeout, errors.Conflict} {
			if !hasKind(got, want) {
				t.Errorf("%q missing from %v; batches compose, so the walk has to be a tree walk rather than one level of it", want, got)
			}
		}
	})
}

func TestKindsOfStopsAtEachBranchOutermostError(t *testing.T) {
	t.Run("a branch contributes one kind, not its whole ancestry", func(t *testing.T) {
		branch := errors.Wrap(errors.New(errors.NotFound, "inner"), errors.Conflict, "outer")
		got := errors.KindsOf(errors.Join(branch, errors.New(errors.Invalid, "other")))
		if !hasKind(got, errors.Conflict) {
			t.Errorf("Conflict missing from %v; a branch's outermost Error is the one that won, exactly as it would have on its own", got)
		}
		if !hasKind(got, errors.Invalid) {
			t.Errorf("Invalid missing from %v", got)
		}
		if hasKind(got, errors.NotFound) {
			t.Errorf("NotFound present in %v; outermost wins per branch, or a deliberate reclassification is undone the moment its error joins a batch", got)
		}
	})
}

func TestKindsOfOnASingleErrorAgreesWithKindOf(t *testing.T) {
	t.Run("a batch of one reports exactly the kind KindOf reports", func(t *testing.T) {
		for _, k := range errors.Kinds {
			err := errors.New(k, "something failed")
			got := errors.KindsOf(err)
			if len(got) != 1 || got[0] != k {
				t.Errorf("KindsOf(New(%q)) = %v, want exactly [%q]; the two questions may differ on a mixed batch by design, never on a single failure", k, got, k)
			}
		}
	})
}

func TestKindsOfOnAnUnclassifiedErrorIsEmpty(t *testing.T) {
	cases := []struct {
		name string
		err  error
	}{
		{"nil carries no classification", nil},
		{"a foreign error carries no classification", stderrors.New("boom")},
		{"an annotated foreign error carries no classification", fmt.Errorf("query: %w", stderrors.New("boom"))},
		{"a batch of foreign errors carries no classification", errors.Join(stderrors.New("a"), stderrors.New("b"))},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := errors.KindsOf(c.err); len(got) != 0 {
				t.Errorf("KindsOf = %v, want nothing; inventing a kind for an error that carries none would put a guess where the transport mapping needs the honest 500", got)
			}
		})
	}
}

func TestKindOfOnABatchReportsAKindThatIsActuallyPresent(t *testing.T) {
	t.Run("the arbitrary answer is still one of the failures in the batch", func(t *testing.T) {
		for _, a := range errors.Kinds {
			for _, b := range errors.Kinds {
				got := errors.KindOf(errors.Join(errors.New(a, "a"), errors.New(b, "b")))
				if got != a && got != b {
					t.Errorf("KindOf(join(%q, %q)) = %q; the answer for a batch is admittedly order-dependent, but it must be one of the failures that occurred rather than an invention", a, b, got)
				}
			}
		}
	})
}

func TestRetryableIsIndependentOfJoinOrder(t *testing.T) {
	t.Run("swapping two branches never changes the retry verdict", func(t *testing.T) {
		for _, a := range errors.Kinds {
			for _, b := range errors.Kinds {
				ab := errors.Retryable(errors.Join(errors.New(a, "a"), errors.New(b, "b")))
				ba := errors.Retryable(errors.Join(errors.New(b, "b"), errors.New(a, "a")))
				if ab != ba {
					t.Errorf("join(%q, %q) = %v but join(%q, %q) = %v; same two failures, opposite retry decision, chosen by argument order — that is not a policy, it is a coin flip, and in a pipeline it decides whether work is retried or dead-lettered", a, b, ab, b, a, ba)
				}
			}
		}
	})

	t.Run("reordering a three-way batch never changes the verdict", func(t *testing.T) {
		build := func(ks ...errors.Kind) error {
			errs := make([]error, 0, len(ks))
			for _, k := range ks {
				errs = append(errs, errors.New(k, "failed"))
			}
			return errors.Join(errs...)
		}
		forward := errors.Retryable(build(errors.Unavailable, errors.Invalid, errors.Timeout))
		reverse := errors.Retryable(build(errors.Timeout, errors.Invalid, errors.Unavailable))
		middle := errors.Retryable(build(errors.Invalid, errors.Unavailable, errors.Timeout))
		if forward != reverse || forward != middle {
			t.Errorf("the same three failures gave %v, %v and %v depending on order; the conservative reading is also the order-independent one, which is the property that actually matters", forward, middle, reverse)
		}
	})
}

func TestRetryableIsConservative(t *testing.T) {
	t.Run("a batch is retryable only if every failure in it is", func(t *testing.T) {
		for _, a := range errors.Kinds {
			for _, b := range errors.Kinds {
				want := a.Retryable() && b.Retryable()
				got := errors.Retryable(errors.Join(errors.New(a, "a"), errors.New(b, "b")))
				if got != want {
					t.Errorf("join(%q, %q) = %v, want %v; a batch containing a permanent failure must not be retryable, because retrying it spends the whole budget re-failing on the one item that cannot succeed", a, b, got, want)
				}
			}
		}
	})
}

func TestRetryableBatch(t *testing.T) {
	build := func(ks ...errors.Kind) error {
		errs := make([]error, 0, len(ks))
		for _, k := range ks {
			errs = append(errs, errors.New(k, "failed"))
		}
		return errors.Join(errs...)
	}
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"all transient: another attempt can plausibly help everything", build(errors.Unavailable, errors.Timeout, errors.RateLimited), true},
		{"one permanent among transients: the permanent one will fail again", build(errors.Unavailable, errors.Invalid, errors.Timeout), false},
		{"contains a cancellation: the caller has gone", build(errors.Unavailable, errors.Canceled), false},
		{"contains a deadline: a per-attempt budget expiring is what retries are for", build(errors.Unavailable, errors.Timeout), true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := errors.Retryable(c.err); got != c.want {
				t.Errorf("Retryable = %v, want %v: %s. Changing this row changes how the pipeline behaves under load", got, c.want, c.name)
			}
		})
	}

	t.Run("a drain and a slow feed are not the same batch", func(t *testing.T) {
		drain := build(errors.Unavailable, errors.Canceled)
		slow := build(errors.Unavailable, errors.Timeout)
		if errors.Retryable(drain) == errors.Retryable(slow) {
			t.Error("a batch containing a cancellation and one containing a deadline got the same verdict; that pair is Timeout versus Canceled doing the only work they exist to do — a drain cancelling in-flight collector work is not retried, a slow feed is")
		}
	})
}

// ---------------------------------------------------------------------------
// TypeOf
// ---------------------------------------------------------------------------

func TestTypeOfNamesWhichFailureNotWhichClass(t *testing.T) {
	slugged := errors.New(errors.Unauthenticated, "authentication required").WithType("token-expired")

	cases := []struct {
		name string
		err  error
		want string
	}{
		{"an error with no slug says so honestly", errors.New(errors.Unauthenticated, "authentication required"), ""},
		{"nil has no slug", nil, ""},
		{"a foreign error has no slug", stderrors.New("boom"), ""},
		{"the slug is readable back", slugged, "token-expired"},
		{"the slug survives annotation", fmt.Errorf("authenticate: %w", slugged), "token-expired"},
		{"the outermost slug wins, as the outermost Error always does", errors.Wrap(slugged, errors.Unauthenticated, "session gone").WithType("session-revoked"), "session-revoked"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := errors.TypeOf(c.err); got != c.want {
				t.Errorf("TypeOf = %q, want %q; no slug is the right answer for a failure whose Kind is all there is to say, and a renderer turns the empty string into about:blank", got, c.want)
			}
		})
	}

	t.Run("three failures of one kind stay distinguishable", func(t *testing.T) {
		missing := errors.New(errors.Unauthenticated, "authentication required").WithType("credentials-missing")
		expired := errors.New(errors.Unauthenticated, "authentication required").WithType("token-expired")
		revoked := errors.New(errors.Unauthenticated, "authentication required").WithType("session-revoked")

		for _, e := range []*errors.Error{missing, expired, revoked} {
			if errors.KindOf(e) != errors.Unauthenticated {
				t.Fatalf("all three must share the kind; the point is that Kind cannot tell them apart, got %q", errors.KindOf(e))
			}
		}
		slugs := map[string]bool{
			errors.TypeOf(missing): true,
			errors.TypeOf(expired): true,
			errors.TypeOf(revoked): true,
		}
		if len(slugs) != 3 {
			t.Errorf("the three slugs collapsed to %d distinct values; no credentials, an expired token and a revoked session are all Unauthenticated, but a client must re-prompt, refresh and sign out respectively and cannot tell them apart from a 401", len(slugs))
		}
	})

	t.Run("a slug is a slug and not a URL", func(t *testing.T) {
		if strings.Contains(errors.TypeOf(slugged), "://") {
			t.Error("the slug looks like a URL; a URL prefix is a fact about the HTTP surface and this package has never known one")
		}
	})
}

// ---------------------------------------------------------------------------
// The stdlib shim
// ---------------------------------------------------------------------------

func TestStdlibPassthroughsReallyPassThrough(t *testing.T) {
	sentinel := errors.New(errors.Conflict, "source already registered")
	foreign := stderrors.New("boom")

	isCases := []struct {
		name        string
		err, target error
	}{
		{"identity through one annotation layer", fmt.Errorf("insert source: %w", sentinel), sentinel},
		{"identity through two annotation layers", fmt.Errorf("register source: %w", fmt.Errorf("insert source: %w", sentinel)), sentinel},
		{"identity through a join", errors.Join(foreign, sentinel), sentinel},
		{"identity through a Wrap", errors.Wrap(sentinel, errors.Internal, "translated"), sentinel},
		{"an unrelated target is not found", fmt.Errorf("insert source: %w", foreign), sentinel},
		{"a nil error finds no target", nil, sentinel},
	}
	for _, c := range isCases {
		t.Run("Is: "+c.name, func(t *testing.T) {
			if got, want := errors.Is(c.err, c.target), stderrors.Is(c.err, c.target); got != want {
				t.Errorf("errors.Is = %v but the standard library says %v; nothing in the shim may ever gain logic, because the day one of them grows a special case every call site in the system silently changes meaning without a single one being edited", got, want)
			}
		})
	}

	t.Run("Unwrap agrees with the standard library", func(t *testing.T) {
		for _, err := range []error{
			fmt.Errorf("insert source: %w", sentinel),
			errors.Wrap(foreign, errors.Conflict, "translated"),
			sentinel,
			foreign,
			nil,
		} {
			if got, want := errors.Unwrap(err), stderrors.Unwrap(err); got != want {
				t.Errorf("errors.Unwrap = %v but the standard library says %v; the shim exists only so a caller importing a Kind does not also have to import two packages named errors", got, want)
			}
		}
	})

	t.Run("As agrees with the standard library and finds the outermost Error", func(t *testing.T) {
		outer := errors.Wrap(errors.New(errors.NotFound, "inner"), errors.Conflict, "outer")
		err := fmt.Errorf("register source: %w", outer)

		var mine *errors.Error
		var theirs *errors.Error
		if got, want := errors.As(err, &mine), stderrors.As(err, &theirs); got != want {
			t.Errorf("errors.As = %v but the standard library says %v", got, want)
		}
		if mine != outer {
			t.Errorf("As bound %v, want the outermost Error; outermost wins throughout this package, and the read side has to agree with the write side about which layer that is", mine)
		}
		if mine != theirs {
			t.Error("the shim and the standard library bound different values")
		}
	})

	t.Run("Join agrees with the standard library", func(t *testing.T) {
		a, b := stderrors.New("a"), stderrors.New("b")
		if got, want := errors.Join(a, b).Error(), stderrors.Join(a, b).Error(); got != want {
			t.Errorf("Join rendered %q, want %q; a kind-aware Join was considered and rejected under the no-logic rule, so this must stay a passthrough", got, want)
		}
	})

	t.Run("Join of nothing is nil", func(t *testing.T) {
		if got := errors.Join(nil, nil); got != nil {
			t.Errorf("Join(nil, nil) = %v, want nil; a batch in which nothing failed is not a failure, and returning a non-nil error for one makes every partial-failure caller report success as an error", got)
		}
	})

	t.Run("a joined error carries no kind of its own", func(t *testing.T) {
		batch := errors.Join(stderrors.New("a"), stderrors.New("b"))
		if got := errors.KindsOf(batch); len(got) != 0 {
			t.Errorf("KindsOf = %v, want nothing; Join passes straight through, so the classification really is lost at the join — which is exactly why KindsOf has to walk the tree rather than Join having to be clever", got)
		}
	})
}
