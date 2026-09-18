package domain_test

import (
	"math"
	"strings"
	"testing"
	"time"

	"github.com/0xsj/overwatch-backend/internal/observation/domain"
	"github.com/0xsj/overwatch-backend/pkg/id"
)

func manual(t *testing.T, content, quote string, start *int) (domain.Manual, error) {
	t.Helper()
	at := time.Now()
	ids := id.NewSequence(at)
	return domain.NewManual(ids.NewID(), ids.NewID(), ids.NewID(), ids.NewID(), ids.NewID(), "A source statement", quote, "paragraph 1", content, start, at)
}
func TestManualCitationUsesCodePointsAndCanSelectRepeatedPassages(t *testing.T) {
	content := "🧭 café then café"
	first, err := manual(t, content, "café", nil)
	if err != nil {
		t.Fatal(err)
	}
	if first.QuoteStart != 2 || first.QuoteEnd != 6 {
		t.Fatalf("first passage offsets: %d:%d", first.QuoteStart, first.QuoteEnd)
	}
	start := 12
	second, err := manual(t, content, "café", &start)
	if err != nil {
		t.Fatal(err)
	}
	if second.QuoteStart != 12 || second.QuoteEnd != 16 || second.Quote != "café" || second.Origin != "manual" {
		t.Fatalf("second passage: %+v", second)
	}
	for _, offset := range []int{-1, 3, 17, math.MaxInt} {
		if _, err := manual(t, content, "café", &offset); err == nil {
			t.Fatalf("accepted wrong range %d", offset)
		}
	}
}
func TestManualCitationPreservesWhitespaceAndDoesNotNormalizeUnicode(t *testing.T) {
	for _, quote := range []string{" \n", "\x00", "e\u0301"} {
		got, err := manual(t, "🧭"+quote+"end", quote, nil)
		if err != nil || got.Quote != quote || got.QuoteStart != 1 {
			t.Fatalf("quote=%q got=%+v err=%v", quote, got, err)
		}
	}
	if _, err := manual(t, "e\u0301", "é", nil); err == nil {
		t.Fatal("normalized Unicode silently changed the citation")
	}
	for _, quote := range []string{"", "absent", strings.Repeat("a", 8001), string([]byte{0xff})} {
		if _, err := manual(t, "source", quote, nil); err == nil {
			t.Fatalf("accepted quote %q", quote)
		}
	}
}
