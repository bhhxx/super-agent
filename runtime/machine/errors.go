package machine

import "fmt"

type UnexpectedEventError struct {
	State State
	Event Event
}

func (e UnexpectedEventError) Error() string {
	return fmt.Sprintf("state %s does not accept event %T", e.State, e.Event)
}

type ProtocolViolationError struct {
	State  State
	Event  Event
	Reason string
}

func (e ProtocolViolationError) Error() string {
	return fmt.Sprintf("protocol violation in state %s for event %T: %s", e.State, e.Event, e.Reason)
}

type InvariantViolationError struct {
	Reason string
}

func (e InvariantViolationError) Error() string {
	return "engine state invariant violation: " + e.Reason
}
