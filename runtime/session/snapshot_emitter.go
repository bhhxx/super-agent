package session

import "sync"

// snapshotEmitter deduplicates approval announcements across repeated
// snapshots of the same pending tool. Reset runs concurrently with a turn,
// so all state transitions take the emitter mutex.
type snapshotEmitter struct {
	mu               sync.Mutex
	emittedMessages  int
	hasApproval      bool
	lastApprovalCall *ToolCall
}

func newSnapshotEmitter() *snapshotEmitter {
	return &snapshotEmitter{}
}

// markApprovalConsumed is called once an approval decision is made, before
// the engine applies it. The next snapshot with a pending tool is a new
// approval, even when the tool call value equals the previous one
// (identical calls in a batch or repeated identical single-call batches).
// hasApproval stays set so a following snapshot without a pending tool
// still emits ToolApprovalCleared.
func (se *snapshotEmitter) markApprovalConsumed() {
	se.mu.Lock()
	defer se.mu.Unlock()
	se.lastApprovalCall = nil
}

// reset re-arms the emitter after a context reset, keeping only the count
// of messages the engine still holds.
func (se *snapshotEmitter) reset(emittedMessages int) {
	se.mu.Lock()
	defer se.mu.Unlock()
	se.emittedMessages = emittedMessages
	se.hasApproval = false
	se.lastApprovalCall = nil
}

func (se *snapshotEmitter) emit(events chan<- SessionEvent, snapshot Snapshot, onMessage func(Message)) {
	se.mu.Lock()
	defer se.mu.Unlock()
	events <- StateChanged{State: snapshot.State}
	if snapshot.PendingTool != nil {
		if !se.hasApproval || se.lastApprovalCall == nil || *se.lastApprovalCall != *snapshot.PendingTool {
			events <- ToolApprovalRequested{
				ToolCall:   *snapshot.PendingTool,
				Request:    permissionRequest(snapshot),
				BatchID:    snapshot.PendingToolBatchID,
				BatchIndex: snapshot.PendingToolBatchIndex,
				BatchTotal: snapshot.PendingToolBatchTotal,
			}
			call := *snapshot.PendingTool
			se.lastApprovalCall = &call
			se.hasApproval = true
		}
	} else {
		if se.hasApproval {
			events <- ToolApprovalCleared{}
		}
		se.hasApproval = false
		se.lastApprovalCall = nil
	}
	messages := snapshot.Messages
	if se.emittedMessages > len(messages) {
		se.emittedMessages = 0
	}
	for _, msg := range messages[se.emittedMessages:] {
		events <- MessageAppended{Message: msg}
		if onMessage != nil {
			onMessage(msg)
		}
	}
	se.emittedMessages = len(messages)
}

func permissionRequest(snapshot Snapshot) PermissionRequest {
	if snapshot.PendingPermission == nil {
		return PermissionRequest{}
	}
	return *snapshot.PendingPermission
}
