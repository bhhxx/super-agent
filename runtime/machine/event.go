package machine

// this file lists all event, which must have isEvent() method
type Event interface {
	isEvent()
}

type UserMessageSubmitted struct {
	Content string
}

func (UserMessageSubmitted) isEvent() {}

type AssistantMessageReceived struct {
	Response ModelResponse
}

func (AssistantMessageReceived) isEvent() {}

type ToolBatchReceived struct {
	Content          string
	Calls            []ToolCall
	ReasoningContent string
}

func (ToolBatchReceived) isEvent() {}

type ToolCallNeedsApproval struct {
	Call    ToolCall
	Request PermissionRequest
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

// AllEvents lists every Event type for registration, serialization, and testing.
var AllEvents = []Event{
	UserMessageSubmitted{},
	AssistantMessageReceived{},
	ToolBatchReceived{},
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
