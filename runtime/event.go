package runtime

// this file lists all event, which must have isEvent() method
type Event interface {
	isEvent()
}

// AllEvents lists every Event type for registration, serialization, and testing.
var AllEvents = []Event{
	UserMessageSubmitted{},
	AssistantMessageReceived{},
	ToolCallsBlockedForApproval{},
	ToolCallsApprovedToRun{},
	ToolResultReceived{},
	ApprovalGranted{},
	ApprovalAlwaysGranted{},
	ApprovalDenied{},
	ErrorOccurred{},
	CancelRequested{},
	ResetRequested{},
	NoMoreToolCalls{},
	NextToolCallNeedsApproval{},
	NextToolCallReadyToRun{},
	EngineReady{},
	AutoApproveToolsRequested{},
}

type UserMessageSubmitted struct {
	Content string
}

func (UserMessageSubmitted) isEvent() {}

type AssistantMessageReceived struct {
	Response ModelResponse
}

func (AssistantMessageReceived) isEvent() {}

type ToolCallsBlockedForApproval struct {
	Content          string
	Calls            []ToolCall
	ReasoningContent string
}

func (ToolCallsBlockedForApproval) isEvent() {}

type ToolCallsApprovedToRun struct {
	Content          string
	Calls            []ToolCall
	ReasoningContent string
}

func (ToolCallsApprovedToRun) isEvent() {}

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

type NextToolCallNeedsApproval struct {
	Call ToolCall
}

func (NextToolCallNeedsApproval) isEvent() {}

type NextToolCallReadyToRun struct {
	Call ToolCall
}

func (NextToolCallReadyToRun) isEvent() {}

type EngineReady struct{}

func (EngineReady) isEvent() {}

type AutoApproveToolsRequested struct {
	Enabled bool
}

func (AutoApproveToolsRequested) isEvent() {}
