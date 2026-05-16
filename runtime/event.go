package runtime

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

type ToolCallsRequested struct {
	Calls            []ToolCall
	ReasoningContent string
	NeedsApproval    bool
}

func (ToolCallsRequested) isEvent() {}

type ToolResultReceived struct {
	Call              ToolCall
	Result            string
	NextCall          *ToolCall
	NextNeedsApproval bool
}

func (ToolResultReceived) isEvent() {}

type ApprovalGranted struct {
	Call ToolCall
}

func (ApprovalGranted) isEvent() {}

type ApprovalDenied struct {
	Call              ToolCall
	NextCall          *ToolCall
	NextNeedsApproval bool
}

func (ApprovalDenied) isEvent() {}

type ErrorOccurred struct{}

func (ErrorOccurred) isEvent() {}

type CancelRequested struct{}

func (CancelRequested) isEvent() {}

type ResetRequested struct{}

func (ResetRequested) isEvent() {}
