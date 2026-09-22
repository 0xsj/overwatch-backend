package root

import (
	"testing"

	assistapp "github.com/0xsj/overwatch-backend/internal/assistance/app"
)

func TestConfiguredConnectionReviewProviderUsesLocalFallbackAndProcessWhenConfigured(t *testing.T) {
	if _, ok := configuredConnectionReviewProvider(Config{}).(assistapp.LocalConnectionReviewProvider); !ok {
		t.Fatal("empty connection-review configuration did not select local provider")
	}
	if _, ok := configuredConnectionReviewProvider(Config{AssistanceBinary: "/bin/sh"}).(assistapp.ProcessConnectionReviewProvider); !ok {
		t.Fatal("configured connection-review binary did not select process provider")
	}
}
