package app_test

import (
	"testing"

	"super-agent/app"
	"super-agent/runtime"
)

func TestTUIAdapterMapsRuntimeState(t *testing.T) {
	engine := runtime.NewEngineWithExecutor(nil, nil)
	conversation := app.NewTUIConversation(runtime.NewSession(engine))
	if got := conversation.Snapshot().AgentStatus; got.Label != "Initializing" || !got.Busy {
		t.Fatalf("initial status = %+v", got)
	}
	if err := engine.Ready(); err != nil {
		t.Fatal(err)
	}
	if got := conversation.Snapshot().AgentStatus; got.Label != "Idle" || got.Busy || got.AwaitingApproval {
		t.Fatalf("ready status = %+v", got)
	}
}
