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
	ToolCallBatchFirstNeedsApproval{},
	ToolCallBatchFirstReadyToRun{},
	ToolResultReceived{},
	ApprovalGranted{},
	ApprovalAlwaysGranted{},
	ApprovalDenied{},
	ErrorOccurred{},
	CancelRequested{},
	ResetRequested{},
	NoMoreToolCalls{},
	NextToolCallAvailable{},
	QueuedToolCallNeedsApproval{},
	QueuedToolCallReadyToRun{},
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

type ToolCallBatchFirstNeedsApproval struct {
	Content          string
	Calls            []ToolCall
	ReasoningContent string
}

func (ToolCallBatchFirstNeedsApproval) isEvent() {}

type ToolCallBatchFirstReadyToRun struct {
	Content          string
	Calls            []ToolCall
	ReasoningContent string
}

func (ToolCallBatchFirstReadyToRun) isEvent() {}

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

type NoMoreToolCalls struct{}

func (NoMoreToolCalls) isEvent() {}

type NextToolCallAvailable struct {
	Call ToolCall
}

func (NextToolCallAvailable) isEvent() {}

type QueuedToolCallNeedsApproval struct {
	Call ToolCall
}

func (QueuedToolCallNeedsApproval) isEvent() {}

type QueuedToolCallReadyToRun struct {
	Call ToolCall
}

func (QueuedToolCallReadyToRun) isEvent() {}

type EngineReady struct{}

func (EngineReady) isEvent() {}
