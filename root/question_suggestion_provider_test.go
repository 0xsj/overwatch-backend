package root

import (
	"testing"

	assistapp "github.com/0xsj/overwatch-backend/internal/assistance/app"
)

func TestConfiguredQuestionSuggestionProviderUsesLocalFallbackAndProcessWhenConfigured(t *testing.T) {
	if _, ok := configuredQuestionSuggestionProvider(Config{}).(assistapp.LocalQuestionSuggestionProvider); !ok {
		t.Fatal("empty question-suggestion configuration did not select local provider")
	}
	if _, ok := configuredQuestionSuggestionProvider(Config{AssistanceBinary: "/bin/sh"}).(assistapp.ProcessQuestionSuggestionProvider); !ok {
		t.Fatal("configured question-suggestion binary did not select process provider")
	}
}
