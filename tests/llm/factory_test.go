package llm_test

import (
	"context"
	"testing"

	. "super-agent/llm"
	"super-agent/runtime"
)

type fakeModel struct{}

func (fakeModel) Next(context.Context, []runtime.Message, []runtime.ToolSpec, func(runtime.StreamChunk)) (runtime.ModelResponse, error) {
	return runtime.ModelResponse{FinalAnswer: "ok"}, nil
}

func TestModelRegistryCreatesRegisteredProvider(t *testing.T) {
	registry := NewModelRegistry()
	registry.Register("fake", func() runtime.Model { return fakeModel{} })

	model, err := registry.New("fake")
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}
	if _, ok := model.(fakeModel); !ok {
		t.Fatalf("model = %T, want fakeModel", model)
	}
}
