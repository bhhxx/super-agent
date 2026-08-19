package session

func (s *Session) startTurn() {
	if s.repository != nil {
		_ = s.repository.StartTurn(s.metaID())
	}
}

func (s *Session) persistMessage(message Message) {
	if s.repository != nil {
		_ = s.repository.SaveMessage(s.metaID(), message)
	}
}

func (s *Session) persistApproval(decision ApprovalDecision) {
	if s.repository == nil {
		return
	}
	var call *ToolCall
	if pending, ok := s.engine.PendingTool(); ok {
		call = &pending
	}
	_ = s.repository.SaveApproval(s.metaID(), decision, call)
}

func (s *Session) persistError(err error) {
	if s.repository != nil && err != nil {
		_ = s.repository.SaveError(s.metaID(), err)
	}
}
