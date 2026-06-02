package execution

import "strconv"

type EffectScheduler struct {
	queue []QueuedEffect
	next  int64
}

func NewEffectScheduler() *EffectScheduler {
	return &EffectScheduler{}
}

func (s *EffectScheduler) Queue(runID RunID, effect Effect) QueuedEffect {
	s.next++
	queued := QueuedEffect{
		RunID:    runID,
		EffectID: EffectID("effect-" + strconv.FormatInt(s.next, 10)),
		Effect:   effect,
	}
	s.queue = append(s.queue, queued)
	return queued
}

func (s *EffectScheduler) Pop() (QueuedEffect, bool) {
	if len(s.queue) == 0 {
		return QueuedEffect{}, false
	}
	effect := s.queue[0]
	s.queue = s.queue[1:]
	return effect, true
}

func (s *EffectScheduler) Clear() {
	s.queue = nil
}

func (s *EffectScheduler) Len() int {
	return len(s.queue)
}
