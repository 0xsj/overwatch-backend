package root

import "testing"

func TestConfiguredSynthesisProviderDefaultsLocalAndSelectsExternalProcess(t *testing.T) {
	local := configuredSynthesisProvider(Config{})
	if local.Name() != "local" || local.Method() != "selected-observations-v1" {
		t.Fatalf("default synthesis provider: %s/%s", local.Name(), local.Method())
	}
	external := configuredSynthesisProvider(Config{SynthesisBinary: "/bin/sh"})
	if external.Name() != "external-process" || external.Method() != "json-selected-observations-v1" {
		t.Fatalf("configured synthesis provider: %s/%s", external.Name(), external.Method())
	}
}
