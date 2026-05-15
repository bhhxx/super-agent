package tools

import (
	"context"
	"errors"

	"super-agent/runtime"
)

type NoTools struct{}

func (NoTools) Specs() []runtime.ToolSpec {
	return nil
}

func (NoTools) Run(context.Context, runtime.ToolCall) (string, error) {
	return "", errors.New("tools are disabled")
}
