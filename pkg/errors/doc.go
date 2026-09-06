// Package errors is the single error vocabulary for the whole repository.
//
// Every failure that crosses a layer boundary is, or wraps, an [Error] carrying
// a [Kind]. The Kind says how a caller should react — retry, fix the input,
// authenticate, reload, give up — and it is the only thing the transport layer
// needs in order to choose a status code. Nothing else here knows about HTTP.
//
// This is the floor of the shared layer, and the one package a domain may
// import. Whatever this package imports, every domain imports transitively, so
// the dependency budget is deliberately tiny.
//
// # Contract
//
//   - Kind is a small closed set. Add one only when a caller must react
//     differently; specifics belong in Msg, in a sentinel, or in Fields.
//   - Msg is safe to show a caller. Anything sensitive — SQL text, feed URLs
//     with keys in them, upstream bodies — goes in Err, which is never returned
//     to one. [Message] additionally hides Msg for Internal.
//   - The OUTERMOST [Error] wins. That is how infrastructure reclassifies a
//     foreign error without touching what the domain decided.
//   - Sentinels are ordinary values. Compare with [Is]; identity survives
//     fmt.Errorf wrapping.
//   - Return or log, never both. The edge logs the full chain once.
//
// # Wrap or fmt.Errorf
//
// [Wrap] changes the classification. That makes it right at an infrastructure
// boundary — translating a foreign error into the vocabulary — and wrong
// everywhere else. To add context without reclassifying, use fmt.Errorf with
// %w. A layer that restates the Kind and guesses wrong silently reclassifies
// the error, which is how a transient failure becomes permanent.
//
// # Use by layer
//
//	domain     return New(NotFound, "source"), or a package sentinel
//	app        return fmt.Errorf("register source: %w", err)
//	infra      translate at the boundary: return Wrap(err, Conflict, "source already registered")
//	transport  status := StatusFor(KindOf(err)); title := Message(err); fields := FieldsOf(err)
//	pipeline   if Retryable(err) { retry } else { dead-letter }
//	tests      assert with Is (sentinels) or KindOf (categories); never match strings
//
// # Batches
//
// Overwatch is a pipeline, so partial failure is the normal case rather than an
// edge: a fifty-item poll that yields one malformed record, one rate-limit and
// one timeout is a Tuesday. Such a batch is reported as a joined error, and a
// joined error has no single Kind.
//
// [KindOf] answers for one failure. On a join it reports whichever [Error] the
// walk reaches first, which depends on the order they were joined in — fine for
// a single error, wrong for a batch.
//
// [KindsOf] and [Retryable] are the batch questions and are order-independent.
// Retryable is conservative: every classified failure must be retryable, because
// retrying a batch containing a permanent failure spends the budget re-failing
// on it.
//
// Cancellation is not a failure of the dependency. A drain that cancels
// in-flight work yields Canceled, which is not retryable — retrying spends work
// on a caller who has already gone.
//
// # Domain errors
//
// This package supplies the vocabulary; each domain supplies the words. A domain
// declares its own failures as sentinels built with [New], choosing the Kind
// once, where the rule lives:
//
//	var (
//		ErrSourceExists   = errors.New(errors.Conflict, "source already registered")
//		ErrFeedUnreachable = errors.New(errors.Unavailable, "feed unreachable")
//	)
//
// When an adapter recognises a foreign error as a known domain condition, wrap
// the sentinel so identity and Kind both survive:
//
//	if isUniqueViolation(err) {
//		return fmt.Errorf("insert source: %w", domain.ErrSourceExists)
//	}
//
// Reserve [Wrap] for foreign errors that map to a category but to no specific
// sentinel. Do not add a Kind for a domain case. Domains never import each
// other's error values; only the Kind is common.
//
// # Not an error
//
// Quarantine is a domain outcome, not a failure. An item scoring below the
// accept threshold was successfully processed into a quarantined item. A Kind
// for it would oblige the transport layer to invent a status code for a
// non-failure, and every caller to catch a success.
//
// # KindOf fails safe; IsKind tells the truth
//
// [KindOf] returns Internal for an error it does not recognise, and — because
// nil is an error it does not recognise — for nil as well. That is correct where
// it is used: an unset or foreign error must become a 500 rather than leak
// through as a 200, and a handler asking "what is this error" already knows it
// has one.
//
// It is wrong as a *predicate*, and the failure is silent. A test written as
//
//	if errors.KindOf(err) != errors.Internal {
//		t.Fatal("want Internal")
//	}
//
// passes when err is nil — which is exactly the case it was written to rule out.
// This was not hypothetical: it was found by mutation-testing internal/feed,
// where deleting a validation left every one of ten subtests green, and the same
// shape was then found in thirty places across four domains.
//
// [IsKind] is the predicate. It is false for nil, because nil is not an error of
// any kind, and asserting with it makes the mistake unwriteable rather than
// merely discouraged.
//
// # Deliberately absent
//
// A stable machine-readable error code. The sentinel is the identity and [Is] is
// the comparison, which needs no second vocabulary to keep in sync. A published
// code catalog is worth building when the first external integrator branches on
// an error, and not before.
package errors
