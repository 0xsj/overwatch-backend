package root

import (
	"testing"

	assistapp "github.com/0xsj/overwatch-backend/internal/assistance/app"
)

func TestConfiguredComparisonProviderUsesLocalFallbackAndProcessWhenConfigured(t *testing.T) {
	if _, ok := configuredComparisonProvider(Config{}).(assistapp.LocalComparisonProvider); !ok {
		t.Fatal("empty comparison configuration did not select local provider")
	}
	if _, ok := configuredComparisonProvider(Config{AssistanceBinary: "/bin/sh"}).(assistapp.ProcessComparisonProvider); !ok {
		t.Fatal("configured comparison binary did not select process provider")
	}
}
