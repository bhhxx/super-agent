package runtime

// this file lists all event, which must have isEvent() method
type Event interface {
	isEvent()
}

// AllEvents lists every Event type for registration, serialization, and testing.
var AllEvents = []Event{
	UserMessageSubmitted{},
	AssistantMessageReceived{},
	ToolCallsReceived{},
	ToolBatchReceived{},
	ToolCallAvailable{},
	ToolCallNeedsApproval{},
	ToolCallReadyToRun{},
	ToolBatchFinished{},
	ToolResultReceived{},
	ApprovalGranted{},
	ApprovalAlwaysGranted{},
	ApprovalDenied{},
	ErrorOccurred{},
	CancelRequested{},
	ResetRequested{},
	EngineReady{},
}

type UserMessageSubmitted struct {
	Content string
}

func (UserMessageSubmitted) isEvent() {}

type AssistantMessageReceived struct {
	Response ModelResponse
}

func (AssistantMessageReceived) isEvent() {}

type ToolCallsReceived struct {
	Content          string
	Calls            []ToolCall
	ReasoningContent string
}

func (ToolCallsReceived) isEvent() {}

type ToolBatchReceived struct {
	Content          string
	Calls            []ToolCall
	ReasoningContent string
}

func (ToolBatchReceived) isEvent() {}

type ToolCallAvailable struct {
	Call ToolCall
}

func (ToolCallAvailable) isEvent() {}

type ToolCallNeedsApproval struct {
	Call ToolCall
}

func (ToolCallNeedsApproval) isEvent() {}

type ToolCallReadyToRun struct {
	Call ToolCall
}

func (ToolCallReadyToRun) isEvent() {}

type ToolBatchFinished struct{}

func (ToolBatchFinished) isEvent() {}

type ToolResultReceived struct {
	Call   ToolCall
	Result string
}

func (ToolResultReceived) isEvent() {}

type ApprovalGranted struct {
	Call ToolCall
}

func (ApprovalGranted) isEvent() {}

type ApprovalAlwaysGranted struct {
	Call ToolCall
}

func (ApprovalAlwaysGranted) isEvent() {}

type ApprovalDenied struct {
	Call ToolCall
}

func (ApprovalDenied) isEvent() {}

type ErrorOccurred struct {
	Err error
}

func (ErrorOccurred) isEvent() {}

type CancelRequested struct{}

func (CancelRequested) isEvent() {}

type ResetRequested struct{}

func (ResetRequested) isEvent() {}

type EngineReady struct{}

func (EngineReady) isEvent() {}
