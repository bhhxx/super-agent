package app_test

import (
	"testing"

	. "super-agent/app"
)

func TestLoadConfigCombinesFlagsAndEnv(t *testing.T) {
	env := map[string]string{
		"LLM_PROVIDER": "claude",
		"NO_TOOLS":     "true",
	}
	cfg := LoadConfig(Flags{YOLO: true}, lookup(env))

	if cfg.Provider != "claude" {
		t.Fatalf("provider = %q, want claude", cfg.Provider)
	}
	if !cfg.NoTools {
		t.Fatal("NoTools = false, want true")
	}
	if !cfg.YOLO {
		t.Fatal("YOLO = false, want true")
	}
}

func lookup(values map[string]string) func(string) (string, bool) {
	return func(key string) (string, bool) {
		value, ok := values[key]
		return value, ok
	}
}
