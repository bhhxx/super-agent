package session

type snapshotEmitter struct {
	emittedMessages int
	emittedApproval *ToolCall
}

func newSnapshotEmitter() *snapshotEmitter {
	return &snapshotEmitter{}
}

func (se *snapshotEmitter) emit(events chan<- SessionEvent, snapshot Snapshot) {
	events <- StateChanged{State: snapshot.State}
	if snapshot.PendingTool != nil {
		if se.emittedApproval == nil || *se.emittedApproval != *snapshot.PendingTool {
			events <- ToolApprovalRequested{
				ToolCall:   *snapshot.PendingTool,
				BatchID:    snapshot.PendingToolBatchID,
				BatchIndex: snapshot.PendingToolBatchIndex,
				BatchTotal: snapshot.PendingToolBatchTotal,
			}
			call := *snapshot.PendingTool
			se.emittedApproval = &call
		}
	} else {
		if se.emittedApproval != nil {
			events <- ToolApprovalCleared{}
		}
		se.emittedApproval = nil
	}
	messages := snapshot.Messages
	if se.emittedMessages > len(messages) {
		se.emittedMessages = 0
	}
	for _, msg := range messages[se.emittedMessages:] {
		events <- MessageAppended{Message: msg}
	}
	se.emittedMessages = len(messages)
}
