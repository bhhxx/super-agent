package runtime

import (
	"context"
	"strconv"
	"sync"
)

type RunID string
type EffectID string

type RunController interface {
	StartRun(parent context.Context) (RunID, context.Context)
	StartNewGeneration()
	CancelRun()
	CurrentRunID() RunID
	CurrentContext(fallback context.Context) context.Context
	IsCurrent(runID RunID) bool
}

type DefaultRunController struct {
	mu     sync.Mutex
	next   int64
	runID  RunID
	ctx    context.Context
	cancel context.CancelFunc
}

func NewDefaultRunController() *DefaultRunController {
	return &DefaultRunController{}
}

func (c *DefaultRunController) StartRun(parent context.Context) (RunID, context.Context) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cancelLocked()
	c.next++
	c.runID = RunID("run-" + strconv.FormatInt(c.next, 10))
	ctx, cancel := context.WithCancel(parent)
	c.ctx = ctx
	c.cancel = cancel
	return c.runID, ctx
}

func (c *DefaultRunController) StartNewGeneration() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cancelLocked()
	c.next++
	c.runID = RunID("run-" + strconv.FormatInt(c.next, 10))
}

func (c *DefaultRunController) CancelRun() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cancelLocked()
	c.next++
	c.runID = RunID("run-" + strconv.FormatInt(c.next, 10))
}

func (c *DefaultRunController) CurrentRunID() RunID {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.runID
}

func (c *DefaultRunController) CurrentContext(fallback context.Context) context.Context {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.ctx == nil {
		return fallback
	}
	return c.ctx
}

func (c *DefaultRunController) IsCurrent(runID RunID) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return runID != "" && runID == c.runID
}

func (c *DefaultRunController) cancelLocked() {
	if c.cancel != nil {
		c.cancel()
		c.cancel = nil
	}
	c.ctx = nil
}
