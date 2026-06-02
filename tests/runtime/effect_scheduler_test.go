package runtime_test

import (
	"testing"

	. "super-agent/runtime"
)

func TestEffectSchedulerQueuesEffectsWithRunAndIncrementingIDs(t *testing.T) {
	scheduler := NewEffectScheduler()

	first := scheduler.Queue("run-1", CallModel{})
	second := scheduler.Queue("run-1", ProcessNextToolCall{})

	if first.RunID != "run-1" {
		t.Fatalf("first RunID = %q, want run-1", first.RunID)
	}
	if first.EffectID != "effect-1" {
		t.Fatalf("first EffectID = %q, want effect-1", first.EffectID)
	}
	if second.EffectID != "effect-2" {
		t.Fatalf("second EffectID = %q, want effect-2", second.EffectID)
	}
	if scheduler.Len() != 2 {
		t.Fatalf("Len = %d, want 2", scheduler.Len())
	}
}

func TestEffectSchedulerPopsInFIFOOrder(t *testing.T) {
	scheduler := NewEffectScheduler()
	first := scheduler.Queue("run-1", CallModel{})
	second := scheduler.Queue("run-1", ProcessNextToolCall{})

	got, ok := scheduler.Pop()
	if !ok {
		t.Fatal("Pop returned ok=false, want true")
	}
	if got != first {
		t.Fatalf("first Pop = %+v, want %+v", got, first)
	}

	got, ok = scheduler.Pop()
	if !ok {
		t.Fatal("second Pop returned ok=false, want true")
	}
	if got != second {
		t.Fatalf("second Pop = %+v, want %+v", got, second)
	}

	if _, ok := scheduler.Pop(); ok {
		t.Fatal("Pop on empty scheduler returned ok=true")
	}
}

func TestEffectSchedulerClearDropsPendingEffects(t *testing.T) {
	scheduler := NewEffectScheduler()
	scheduler.Queue("run-1", CallModel{})
	scheduler.Queue("run-1", ProcessNextToolCall{})

	scheduler.Clear()

	if scheduler.Len() != 0 {
		t.Fatalf("Len = %d, want 0", scheduler.Len())
	}
	if _, ok := scheduler.Pop(); ok {
		t.Fatal("Pop after Clear returned ok=true")
	}
}
