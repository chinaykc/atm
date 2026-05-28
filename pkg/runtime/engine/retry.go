package engine

import (
	"context"
	"github.com/chinaykc/atm/pkg/integration/agent"
	"github.com/chinaykc/atm/pkg/lang/compiler"
	"io"
)

func (e *Engine) executeAgent(ctx context.Context, todoPath, prompt string, opts compiler.RunOptions, stdout, stderr io.Writer) (agent.ExecuteResult, error) {
	var result agent.ExecuteResult
	var err error
	for attempt := 0; ; attempt++ {
		result, err = e.runner.Execute(ctx, todoPath, prompt, opts, stdout, stderr)
		if err == nil || !e.shouldRetryAgentError(ctx, err, attempt) {
			return result, err
		}
		writeATMEvent(stderr, "retry", "agent run failed with retryable error; retry %d/%d: %v", attempt+1, e.retries, err)
	}
}

func (e *Engine) checkAgent(ctx context.Context, todoPath, prompt, condition string, opts compiler.RunOptions, stdout, stderr io.Writer) (bool, error) {
	var passed bool
	var err error
	for attempt := 0; ; attempt++ {
		passed, err = e.runner.Check(ctx, todoPath, prompt, condition, opts, stdout, stderr)
		if err == nil || !e.shouldRetryAgentError(ctx, err, attempt) {
			return passed, err
		}
		writeATMEvent(stderr, "retry", "agent check failed with retryable error; retry %d/%d: %v", attempt+1, e.retries, err)
	}
}

func (e *Engine) shouldRetryAgentError(ctx context.Context, err error, attempt int) bool {
	if err == nil || ctx.Err() != nil || e.retries <= attempt {
		return false
	}
	return agent.IsRetryableError(err)
}
