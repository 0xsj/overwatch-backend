// Author-written, against a live database, for owed item A.
//
// `POST /v1/sessions` was the only unauthenticated endpoint taking a password
// and the only one with no budget at all — credential stuffing against a leaked
// address list cost an attacker one request per guess and cost this server
// nothing to refuse. Only the three mail-sending endpoints called `spend`.
package root

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/0xsj/overwatch-backend/pkg/limit"
)

// stopped does not advance, so this measures the BURST and never accidentally
// measures the refill.
type stopped struct{ at time.Time }

func (s stopped) Now() time.Time { return s.at }

func budget(burst int) *limit.Limiter {
	return limit.New(limit.Config{
		Rule:  limit.Rule{Burst: burst, Every: 30 * time.Second},
		Clock: stopped{at: time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)},
	})
}

// **THE test of owed item A.** Wrong passwords exhaust the budget, and the next
// attempt is refused before the password is checked at all.
func TestFailedSignInsAreRateLimitedAndSuccessesAreNot(t *testing.T) {
	guessBudget = budget(3)
	s := tracedSystem(t)
	// A VERIFIED account, because an unverified one is refused for a different
	// reason and this test would then be measuring nothing.
	_, auth := s.signUp(t, "sam@example.com")
	verify(t, s, auth)

	wrong := `{"email":"sam@example.com","password":"not the passphrase"}`
	right := `{"email":"sam@example.com","password":"a passphrase nobody guesses"}`

	for n := 0; n < 3; n++ {
		res := s.post(t, "/v1/sessions", wrong, nil)
		if res.StatusCode != http.StatusUnauthorized {
			t.Fatalf("guess %d: got %d, want 401", n+1, res.StatusCode)
		}
	}
	// The budget is spent. The next one never reaches the password check.
	res := s.post(t, "/v1/sessions", wrong, nil)
	if res.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("the fourth guess: got %d, want 429", res.StatusCode)
	}
	// AND THE RIGHT PASSWORD IS REFUSED TOO, which is the point: an attacker
	// who guesses correctly on attempt four is still stopped, and a legitimate
	// user who has burned the budget waits like anybody else.
	if res := s.post(t, "/v1/sessions", right, nil); res.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("a correct password on a spent budget: got %d, want 429", res.StatusCode)
	}
}

// **A SUCCESS DOES NOT SPEND.** Charging every attempt would lock out somebody
// signing in on four devices after a password change, and what is being limited
// is guessing rather than using.
func TestASuccessfulSignInSpendsNothing(t *testing.T) {
	guessBudget = budget(2)
	s := tracedSystem(t)
	_, auth := s.signUp(t, "sam@example.com")
	verify(t, s, auth)

	right := `{"email":"sam@example.com","password":"a passphrase nobody guesses"}`
	for n := 0; n < 6; n++ {
		res := s.post(t, "/v1/sessions", right, nil)
		if res.StatusCode != http.StatusCreated {
			t.Fatalf("sign-in %d: got %d, want 201 — a success must not spend", n+1, res.StatusCode)
		}
	}
}

// signInFrom drives the real mux from a NAMED CLIENT ADDRESS, which
// `traced.post` cannot do — every request it sends comes from the same
// `192.0.2.1` httptest picks. Without varying it the address key masks the email
// key completely, and a mutation round found exactly that: removing the email
// key entirely, and un-folding it, both survived.
func signInFrom(t *testing.T, s traced, from, email, password string) int {
	t.Helper()
	body := `{"email":"` + email + `","password":"` + password + `"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/sessions", strings.NewReader(body))
	req.Header.Set("content-type", "application/json")
	req.RemoteAddr = from + ":54321"
	rec := httptest.NewRecorder()
	s.handler.ServeHTTP(rec, req)
	return rec.Result().StatusCode
}

// **THE EMAIL KEY DOES SOMETHING.** An attacker moving between hosts to guess
// one address is stopped by it, and nothing else would stop them: every request
// comes from a fresh address, so the address budget is untouched.
func TestGuessingOneAddressFromManyHostsIsStillRefused(t *testing.T) {
	guessBudget = budget(3)
	s := tracedSystem(t)
	_, auth := s.signUp(t, "sam@example.com")
	verify(t, s, auth)

	for n, host := range []string{"203.0.113.1", "203.0.113.2", "203.0.113.3"} {
		if got := signInFrom(t, s, host, "sam@example.com", "wrong"); got != http.StatusUnauthorized {
			t.Fatalf("guess %d from %s: got %d, want 401", n+1, host, got)
		}
	}
	// A FOURTH HOST, never seen before, so its address budget is full — and the
	// EMAIL budget is spent.
	if got := signInFrom(t, s, "203.0.113.4", "sam@example.com", "wrong"); got != http.StatusTooManyRequests {
		t.Fatalf("a fresh host guessing a spent address: got %d, want 429", got)
	}
	// And a DIFFERENT email from that same fresh host still works, which is what
	// makes this the email key and not a global one.
	_, kit := s.signUp(t, "kit@example.com")
	verify(t, s, kit)
	if got := signInFrom(t, s, "203.0.113.4", "kit@example.com",
		"a passphrase nobody guesses"); got != http.StatusCreated {
		t.Fatalf("an unrelated account from a fresh host: got %d, want 201", got)
	}
}

// The email key is FOLDED, because a rate limit must not depend on an address
// being valid: `SAM@Example.com ` and `sam@example.com` are the same guess
// whatever the domain layer later decides about either.
func TestTheEmailBudgetIsSpentWhateverTheSpelling(t *testing.T) {
	guessBudget = budget(2)
	s := tracedSystem(t)
	_, auth := s.signUp(t, "sam@example.com")
	verify(t, s, auth)

	for n, spelling := range []string{"sam@example.com", "SAM@Example.com"} {
		if got := signInFrom(t, s, "203.0.113."+string(rune('1'+n)), spelling, "wrong"); got != http.StatusUnauthorized {
			t.Fatalf("%q: got %d, want 401", spelling, got)
		}
	}
	// Two spellings, one budget — so a third attempt from a FRESH host is
	// refused. Un-folded, each spelling would have its own budget and this
	// would be a 401.
	if got := signInFrom(t, s, "203.0.113.9", "  sam@EXAMPLE.com  ", "wrong"); got != http.StatusTooManyRequests {
		t.Fatalf("a differently-spelled address must share the budget: got %d, want 429", got)
	}
}

// TWO KEYS, either of which can refuse, and this pins the COST of that rather
// than the benefit: every request from this harness shares one address, so
// burning it on one account refuses the next account too.
//
// That is correct — one host guessing many addresses is what stuffing IS — and
// it is the trade the address key buys. Naming the test after the cost is
// deliberate: the first draft called it "does not lock another" and asserted
// the opposite of its own name.
func TestASpentAddressBudgetCoversEveryAccountBehindIt(t *testing.T) {
	guessBudget = budget(2)
	s := tracedSystem(t)
	_, sam := s.signUp(t, "sam@example.com")
	verify(t, s, sam)
	_, kit := s.signUp(t, "kit@example.com")
	verify(t, s, kit)

	for n := 0; n < 3; n++ {
		s.post(t, "/v1/sessions", `{"email":"sam@example.com","password":"wrong"}`, nil)
	}
	// Sam's budget is gone — but that is the shared IP key too, so the second
	// account is refused as well. THAT IS CORRECT and it is the trade the
	// address key buys: one host guessing many addresses is what stuffing is.
	res := s.post(t, "/v1/sessions",
		`{"email":"kit@example.com","password":"a passphrase nobody guesses"}`, nil)
	if res.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("a spent ADDRESS budget covers every account behind it: got %d", res.StatusCode)
	}
}
