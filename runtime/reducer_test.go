package runtime

import "testing"

type unknownMutation struct{}

func (unknownMutation) isMutation() {}

func TestDefaultReducerPanicsOnUnknownMutation(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("DefaultReducer.Apply did not panic for unknown mutation")
		}
	}()

	var state EngineState
	var effectQueue []QueuedEffect
	DefaultReducer{}.Apply(&state, &effectQueue, unknownMutation{})
}
