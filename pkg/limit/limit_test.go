package limit_test

import (
	"context"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/0xsj/overwatch-backend/pkg/limit"
)

// A limiter tested against wall time is a test that sleeps, and a test that
// sleeps is a test somebody deletes.
type dial struct {
	mu sync.Mutex
	at time.Time
}

func newDial() *dial { return &dial{at: time.Date(2026, 9, 7, 12, 0, 0, 0, time.UTC)} }

func (d *dial) Now() time.Time {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.at
}

func (d *dial) advance(by time.Duration) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.at = d.at.Add(by)
}

func TestABurstIsSpentThenRefillsOneTokenAtATime(t *testing.T) {
	clk := newDial()
	l := limit.New(limit.Config{Rule: limit.Rule{Burst: 3, Every: time.Second}, Clock: clk})

	for i := 0; i < 3; i++ {
		if !l.Allow("example.com") {
			t.Fatalf("refused request %d of a burst of 3", i+1)
		}
	}
	if l.Allow("example.com") {
		t.Fatal("a fourth request was allowed with no time passing")
	}

	clk.advance(999 * time.Millisecond)
	if l.Allow("example.com") {
		t.Error("allowed just before a token existed")
	}
	clk.advance(time.Millisecond)
	if !l.Allow("example.com") {
		t.Error("refused once a full interval had passed")
	}
}

// The thing a per-source interval cannot do: two engagements scanning one
// company share the budget, because the thing being protected is not ours.
func TestKeysAreIndependentAndTheKeyIsTheHost(t *testing.T) {
	clk := newDial()
	l := limit.New(limit.Config{Rule: limit.Rule{Burst: 1, Every: time.Minute}, Clock: clk})

	if !l.Allow("acme.example") || !l.Allow("other.example") {
		t.Fatal("two hosts did not each get their own budget")
	}
	if l.Allow("acme.example") {
		t.Error("a second request to one host was allowed")
	}
	if l.Allow("other.example") {
		t.Error("keys are sharing a bucket")
	}
}

func TestRefillIsCappedAtTheBurst(t *testing.T) {
	clk := newDial()
	l := limit.New(limit.Config{Rule: limit.Rule{Burst: 2, Every: time.Second}, Clock: clk})

	l.Allow("h")
	l.Allow("h")
	clk.advance(time.Hour)

	if got := l.Tokens("h"); got != 2 {
		t.Errorf("%v tokens after an hour idle, want the burst of 2 — an idle key must not bank an hour of requests", got)
	}
}

func TestWaitBlocksUntilABudgetExistsRatherThanFailing(t *testing.T) {
	clk := newDial()
	l := limit.New(limit.Config{Rule: limit.Rule{Burst: 1, Every: 20 * time.Millisecond}, Clock: clk})

	if err := l.Wait(context.Background(), "h"); err != nil {
		t.Fatal(err)
	}
	// The clock only moves when the test moves it, so the waiter must actually
	// be waiting rather than spinning through.
	done := make(chan error, 1)
	go func() { done <- l.Wait(context.Background(), "h") }()

	select {
	case <-done:
		t.Fatal("Wait returned with no budget and no time passing")
	case <-time.After(50 * time.Millisecond):
	}
	clk.advance(20 * time.Millisecond)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Wait did not return once the budget refilled")
	}
}

func TestWaitAnswersACancelledContext(t *testing.T) {
	clk := newDial()
	l := limit.New(limit.Config{Rule: limit.Rule{Burst: 1, Every: time.Hour}, Clock: clk})
	if err := l.Wait(context.Background(), "h"); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if err := l.Wait(ctx, "h"); err == nil {
		t.Fatal("Wait ignored a cancelled context and would have blocked for an hour")
	}
}

// Hosts come from targets and addresses come from whoever is connecting, so an
// unbounded map is one request per key away from taking the process down.
func TestTheKeyspaceIsBoundedAndEvictingFullBucketsCostsNothing(t *testing.T) {
	clk := newDial()
	l := limit.New(limit.Config{
		Rule: limit.Rule{Burst: 2, Every: time.Second}, Clock: clk, MaxKeys: 64,
	})

	// Spend one token on a key we care about, then flood with distinct keys.
	if !l.Allow("kept") {
		t.Fatal("first request refused")
	}
	for i := 0; i < 5000; i++ {
		l.Allow("flood-" + strconv.Itoa(i))
	}

	// Every flooded key is at burst-1 and refills; a full bucket is
	// indistinguishable from a missing one, so nothing observable was lost.
	if !l.Allow("kept") {
		t.Error("a live key lost its budget entirely to the sweep")
	}
	if !l.Allow("flood-4999") {
		t.Error("a recently seen key was refused after eviction")
	}
}

func TestConstructionRefusesAPolicyThatCannotWork(t *testing.T) {
	clk := newDial()
	for name, call := range map[string]func(){
		"a burst of zero":    func() { limit.New(limit.Config{Rule: limit.Rule{Burst: 0, Every: time.Second}, Clock: clk}) },
		"no refill interval": func() { limit.New(limit.Config{Rule: limit.Rule{Burst: 1}, Clock: clk}) },
		"a nil clock":        func() { limit.New(limit.Config{Rule: limit.Rule{Burst: 1, Every: time.Second}}) },
	} {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Error("constructed")
				}
			}()
			call()
		})
	}
}

func TestConcurrentCallersDoNotOverspend(t *testing.T) {
	clk := newDial()
	l := limit.New(limit.Config{Rule: limit.Rule{Burst: 50, Every: time.Hour}, Clock: clk})

	var wg sync.WaitGroup
	var mu sync.Mutex
	granted := 0
	for i := 0; i < 200; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if l.Allow("h") {
				mu.Lock()
				granted++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if granted != 50 {
		t.Errorf("granted %d of a burst of 50", granted)
	}
}
