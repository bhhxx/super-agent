package llm_test

import (
	"context"
	"testing"

	. "super-agent/llm"
	"super-agent/runtime"
)

type fakeModel struct{}

func (fakeModel) Next(context.Context, []runtime.Message, []runtime.ToolSpec, func(runtime.StreamChunk)) (runtime.ModelResponse, error) {
	return runtime.ModelResponse{Content: "ok"}, nil
}

func TestModelRegistryCreatesRegisteredProvider(t *testing.T) {
	registry := NewModelRegistry()
	registry.RegisterConfigured("fake", func(ProviderConfig) runtime.Model { return fakeModel{} })

	model, err := registry.Create("fake", ProviderConfig{})
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}
	if _, ok := model.(fakeModel); !ok {
		t.Fatalf("model = %T, want fakeModel", model)
	}
}
