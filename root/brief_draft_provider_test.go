package root

import (
	"testing"

	assistapp "github.com/0xsj/overwatch-backend/internal/assistance/app"
)

func TestConfiguredBriefDraftProviderUsesLocalFallbackAndProcessWhenConfigured(t *testing.T) {
	if _, ok := configuredBriefDraftProvider(Config{}).(assistapp.LocalBriefDraftProvider); !ok {
		t.Fatal("empty brief-draft configuration did not select local provider")
	}
	if _, ok := configuredBriefDraftProvider(Config{AssistanceBinary: "/bin/sh"}).(assistapp.ProcessBriefDraftProvider); !ok {
		t.Fatal("configured brief-draft binary did not select process provider")
	}
}
