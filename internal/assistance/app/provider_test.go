package app_test

import (
	"context"
	"testing"

	"github.com/0xsj/overwatch-backend/internal/assistance/app"
)

func TestLocalSentenceProviderReturnsExactRuneRanges(t *testing.T) {
	content := "📍 East Quay is closed. A replacement route is running."
	got, err := (app.LocalSentenceProvider{}).Extract(context.Background(), app.Input{Content: content})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Quote != "📍 East Quay is closed." || got[0].QuoteStart != 0 || got[0].QuoteEnd != 22 {
		t.Fatalf("unexpected sentence proposals: %+v", got)
	}
	if got[1].QuoteStart != 23 || got[1].QuoteEnd != 54 {
		t.Fatalf("unexpected second rune range: %+v", got[1])
	}
}
